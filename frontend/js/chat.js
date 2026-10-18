// Copyright (C) 2026 Elodine / GoblinCorps. AGPL-3.0-or-later.
// Presentation adapted from GobboNet chat.html @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f.
// Modified by the Labbonet project, 2026-10-17: safe rendering and Phase 1 generation API.
// Chat thread: streaming output, stop button, collapsible reasoning
// (ARCHITECTURE.md section 3, section 4.2).
import { api, streamGenerate } from "./api.js";
import { renderMarkdown } from "./render.js";
import { mountModelSelector } from "./models.js";

export function mountChatPanel(panel) {
  panel.innerHTML = "";
  const empty = document.createElement("div");
  empty.className = "welcome";
  const heading = document.createElement("h2");
  heading.textContent = "SYSTEMS ONLINE";
  const hint = document.createElement("p");
  hint.textContent = "// select a thread or create a new one";
  empty.append(heading, hint);
  panel.appendChild(empty);
  const title = document.getElementById("thread-title");
  title.value = "No thread selected";
  title.disabled = true;
  document.getElementById("model-slot").replaceChildren();
}

// loadConversationIntoPanel replaces panel's content with the full
// chat UI for one conversation: header (title, model selector,
// delete), the message thread, and the composer.
export async function loadConversationIntoPanel(panel, conversationId, callbacks) {
  panel.innerHTML = "";

  const titleInput = document.getElementById("thread-title");
  titleInput.disabled = true;
  const modelSlot = document.getElementById("model-slot");

  const thread = document.createElement("div");
  thread.className = "messages";

  const composer = document.createElement("form");
  composer.className = "input-area";
  const textarea = document.createElement("textarea");
  textarea.id = "msg-input";
  textarea.rows = 1;
  textarea.placeholder = "Type your message... (Enter to send, Shift+Enter for newline)";
  textarea.setAttribute("aria-label", "Message");
  textarea.required = true;
  const sendBtn = document.createElement("button");
  sendBtn.type = "submit";
  sendBtn.id = "send-btn";
  sendBtn.className = "btn btn-fill";
  sendBtn.textContent = "Send";
  const stopBtn = document.createElement("button");
  stopBtn.type = "button";
  stopBtn.id = "stop-btn";
  stopBtn.className = "btn";
  stopBtn.textContent = "Stop";
  stopBtn.hidden = true;
  composer.append(textarea, sendBtn, stopBtn);

  panel.append(thread, composer);

  const conv = await api.getConversation(conversationId);
  titleInput.value = conv.title;
  let currentModelId = conv.model_id;
  sendBtn.disabled = !conv.endpoint_id || !conv.model_id;

  function renderMessageNode(msg) {
    const el = document.createElement("div");
    el.className = `message message-${msg.role} status-${msg.status || "complete"}`;

    const messageHeader = document.createElement("div");
    messageHeader.className = "msg-header";
    const avatar = document.createElement("div");
    avatar.className = "msg-avatar";
    avatar.textContent = msg.role === "user" ? "U" : "AI";
    const role = document.createElement("span");
    role.className = "message-role";
    role.textContent = msg.role === "user" ? "You" : "Assistant";
    messageHeader.append(avatar, role);
    el.append(messageHeader);

    if (msg.reasoning) {
      const details = document.createElement("details");
      details.className = "cot-block";
      const summary = document.createElement("summary");
      summary.textContent = "Reasoning";
      summary.className = "cot-summary";
      const body = document.createElement("div");
      body.className = "cot-content";
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
    sendBtn.disabled = true;
    try {
      await api.patchConversation(conversationId, { endpoint_id: endpointId, model_id: modelId });
      currentModelId = modelId;
    } catch (err) {
      alert(`Could not switch model: ${err.message}`);
      throw err;
    } finally {
      sendBtn.disabled = !currentModelId;
    }
  });

  const saveTitle = async () => {
    try {
      await api.patchConversation(conversationId, { title: titleInput.value });
      callbacks.onRenamed?.();
    } catch (err) { alert(err.message); }
  };
  titleInput.onchange = saveTitle;
  titleInput.onkeydown = (event) => {
    if (event.key === "Enter") { event.preventDefault(); saveTitle(); }
  };
  titleInput.disabled = false;

  let currentGenerationId = null;
  let abortController = null;

  async function send(content, confirmLoad) {
    sendBtn.disabled = true;
    const modelSelect = modelSlot.querySelector("select");
    if (modelSelect) modelSelect.disabled = true;
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
      if (modelSelect) modelSelect.disabled = false;
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
    if (sendBtn.disabled) return;
    const content = textarea.value.trim();
    if (!content) return;
    textarea.value = "";
    send(content, false);
  });

  textarea.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      if (!sendBtn.disabled) composer.requestSubmit();
    }
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
