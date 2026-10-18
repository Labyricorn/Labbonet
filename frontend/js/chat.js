// Chat thread: streaming output, stop button, collapsible reasoning
// (ARCHITECTURE.md section 3, section 4.2).
import { api, streamGenerate } from "./api.js";
import { renderMarkdown } from "./render.js";
import { mountModelSelector } from "./models.js";

export function mountChatPanel(panel) {
  panel.innerHTML = "";
  const empty = document.createElement("p");
  empty.className = "empty-state";
  empty.textContent = "Select or create a conversation.";
  panel.appendChild(empty);
}

// loadConversationIntoPanel replaces panel's content with the full
// chat UI for one conversation: header (title, model selector,
// delete), the message thread, and the composer.
export async function loadConversationIntoPanel(panel, conversationId, callbacks) {
  panel.innerHTML = "";

  const header = document.createElement("div");
  header.className = "chat-header";
  const titleInput = document.createElement("input");
  titleInput.className = "chat-title";
  const modelSlot = document.createElement("div");
  modelSlot.className = "chat-model-slot";
  const deleteBtn = document.createElement("button");
  deleteBtn.type = "button";
  deleteBtn.className = "danger";
  deleteBtn.textContent = "Delete";
  header.append(titleInput, modelSlot, deleteBtn);

  const thread = document.createElement("div");
  thread.className = "chat-thread";

  const composer = document.createElement("form");
  composer.className = "chat-composer";
  const textarea = document.createElement("textarea");
  textarea.placeholder = "Message…";
  textarea.required = true;
  const sendBtn = document.createElement("button");
  sendBtn.type = "submit";
  sendBtn.textContent = "Send";
  const stopBtn = document.createElement("button");
  stopBtn.type = "button";
  stopBtn.textContent = "Stop";
  stopBtn.hidden = true;
  composer.append(textarea, sendBtn, stopBtn);

  panel.append(header, thread, composer);

  const conv = await api.getConversation(conversationId);
  titleInput.value = conv.title;
  let currentModelId = conv.model_id;

  function renderMessageNode(msg) {
    const el = document.createElement("div");
    el.className = `message message-${msg.role} status-${msg.status || "complete"}`;

    if (msg.reasoning) {
      const details = document.createElement("details");
      details.className = "reasoning";
      const summary = document.createElement("summary");
      summary.textContent = "Reasoning";
      const body = document.createElement("div");
      body.className = "reasoning-body";
      body.appendChild(renderMarkdown(msg.reasoning));
      details.append(summary, body);
      el.appendChild(details);
    }

    const content = document.createElement("div");
    content.className = "message-content";
    content.appendChild(renderMarkdown(msg.content));
    el.appendChild(content);

    if (msg.status && msg.status !== "complete") {
      const badge = document.createElement("span");
      badge.className = "status-badge";
      badge.textContent = msg.status;
      el.appendChild(badge);
    }
    return el;
  }

  function renderThread(messages) {
    thread.innerHTML = "";
    for (const msg of messages) thread.appendChild(renderMessageNode(msg));
    thread.scrollTop = thread.scrollHeight;
  }

  renderThread(conv.messages);

  await mountModelSelector(modelSlot, conv.endpoint_id, conv.model_id, async (endpointId, modelId) => {
    await api.patchConversation(conversationId, { endpoint_id: endpointId, model_id: modelId });
    currentModelId = modelId;
  });

  titleInput.addEventListener("change", async () => {
    await api.patchConversation(conversationId, { title: titleInput.value });
    callbacks.onRenamed?.();
  });

  deleteBtn.addEventListener("click", async () => {
    if (!confirm("Delete this conversation and all its messages?")) return;
    await api.deleteConversation(conversationId);
    callbacks.onDeleted?.();
  });

  let currentGenerationId = null;
  let abortController = null;

  async function send(content, confirmLoad) {
    sendBtn.disabled = true;
    stopBtn.hidden = false;
    abortController = new AbortController();

    const userEl = renderMessageNode({ role: "user", content, status: "complete" });
    thread.appendChild(userEl);
    const assistantMsg = { role: "assistant", content: "", reasoning: "", status: "streaming" };
    let assistantEl = renderMessageNode(assistantMsg);
    thread.appendChild(assistantEl);
    thread.scrollTop = thread.scrollHeight;

    try {
      await streamGenerate(
        conversationId,
        { content, confirm_load: confirmLoad },
        (ev) => {
          if (ev.generation_id) currentGenerationId = ev.generation_id;
          if (ev.kind === "delta") {
            assistantMsg.content += ev.content || "";
            assistantMsg.reasoning += ev.reasoning || "";
            const refreshed = renderMessageNode(assistantMsg);
            assistantEl.replaceWith(refreshed);
            assistantEl = refreshed;
            thread.scrollTop = thread.scrollHeight;
          }
        },
        abortController.signal,
      );
    } catch (err) {
      if (err.status === 409 && /not.loaded/i.test(err.message) && !confirmLoad) {
        thread.removeChild(userEl);
        thread.removeChild(assistantEl);
        const proceed = confirm(
          `Model "${currentModelId}" is not loaded. Loading it now may take a while and could evict ` +
            "another client's model from memory. Load it and continue?",
        );
        if (proceed) {
          sendBtn.disabled = false;
          stopBtn.hidden = true;
          await send(content, true);
          return;
        }
      } else if (err.name !== "AbortError") {
        alert(`Generation failed: ${err.message}`);
      }
    } finally {
      sendBtn.disabled = false;
      stopBtn.hidden = true;
      currentGenerationId = null;
      try {
        const fresh = await api.getConversation(conversationId);
        renderThread(fresh.messages);
      } catch {
        // The conversation may have just been deleted from elsewhere;
        // leave the last rendered state as is.
      }
    }
  }

  composer.addEventListener("submit", (e) => {
    e.preventDefault();
    const content = textarea.value.trim();
    if (!content) return;
    textarea.value = "";
    send(content, false);
  });

  // A-C4: Stop cancels the running generation through the API (marked
  // "cancelled"), not just the browser's own fetch (which alone would
  // only ever produce "interrupted").
  stopBtn.addEventListener("click", async () => {
    if (currentGenerationId) {
      try {
        await api.cancelGeneration(currentGenerationId);
      } catch {
        // Best effort; aborting the fetch below still stops the UI.
      }
    }
    abortController?.abort();
  });
}
