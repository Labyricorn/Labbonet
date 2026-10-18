// Copyright (C) 2026 Elodine / GoblinCorps. AGPL-3.0-or-later.
// Presentation adapted from GobboNet chat.html @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f.
// Modified by the Labbonet project, 2026-10-17: safe rendering and Phase 1 generation API.
// Chat thread: streaming output, stop button, collapsible reasoning
// (ARCHITECTURE.md section 3, section 4.2).
// Labbonet additions/modifications: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Upstream credit covers presentation structure/design; runtime/API/state/security code is Labbonet work.
// Message actions (edit, copy, reroll, fork, delete, variant navigator) reuse the pinned msg-action and
// variant-nav presentation classes; branch/revision behavior is Labbonet-original.
// Element-by-element source and authorship boundary: docs/REUSE-LOG.md and root NOTICE.md.
import { mountCastControls, decorateSpeaker, markDialog } from "./cast.js";
import { api, streamGenerate } from "./api.js";
import { renderMarkdown } from "./render.js";
import { mountModelSelector } from "./models.js";

let castLifecycle;

export function mountChatPanel(panel) {
  castLifecycle?.abort();
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

function actionButton(label, cls, title, handler) {
  const btn = document.createElement("button");
  btn.type = "button";
  btn.className = `msg-action-btn ${cls}`.trim();
  btn.textContent = label;
  btn.title = title;
  btn.addEventListener("click", handler);
  return btn;
}

// loadConversationIntoPanel replaces panel's content with the full
// chat UI for one conversation: header (title, model selector,
// delete), the message thread, and the composer.
export async function loadConversationIntoPanel(panel, conversationId, callbacks) {
  castLifecycle?.abort();
  const lifecycle = new AbortController(); castLifecycle = lifecycle;
  let activeCharacter, activePersona;
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

  const castSlot = document.createElement("div"); castSlot.className = "cast-slot";
  const threadWrap = document.createElement("div"); threadWrap.className = "cast-thread-wrap";
  const background = document.createElement("img"); background.className = "cast-background"; background.alt = ""; background.hidden = true;
  threadWrap.append(background, thread);
  panel.append(castSlot, threadWrap, composer);

  const conv = await api.getConversation(conversationId);
  titleInput.value = conv.title;
  let currentModelId = conv.model_id;
  // The tree revision this tab last saw; every branch edit and send echoes it so a stale
  // tab gets a conflict instead of overwriting another tab's work.
  let revision = conv.revision;
  let messages = conv.messages;
  let streaming = false;
  let editing = null;
  sendBtn.disabled = !conv.endpoint_id || !conv.model_id;

  function applyConversation(fresh) {
    revision = fresh.revision;
    messages = fresh.messages;
  }

  async function reload() {
    const fresh = await api.getConversation(conversationId);
    applyConversation(fresh);
    renderThread();
  }

  // Run one branch edit. A conflict means another tab changed the thread: say so and
  // show the current state rather than retrying blindly.
  async function mutate(action) {
    try {
      await action();
    } catch (err) {
      alert(err.status === 409 ? `${err.message}. The thread was reloaded.` : err.message);
    }
    try { await reload(); } catch { /* conversation may have been deleted elsewhere */ }
  }

  async function copyText(text, button) {
    try {
      await navigator.clipboard.writeText(text);
      const old = button.textContent;
      button.textContent = "COPIED";
      setTimeout(() => { button.textContent = old; }, 1200);
    } catch {
      alert("Copy is not available in this browser context.");
    }
  }

  function variantNav(msg) {
    const nav = document.createElement("span");
    nav.className = "variant-nav";
    const index = msg.variant_index;
    const total = msg.variants.length;
    const step = (delta) => () => mutate(() => api.activateMessage(conversationId, msg.variants[index + delta], revision));
    const prev = document.createElement("button");
    prev.type = "button"; prev.className = "variant-nav-btn"; prev.textContent = "◀";
    prev.setAttribute("aria-label", "Previous variant"); prev.disabled = index === 0;
    prev.addEventListener("click", step(-1));
    const label = document.createElement("span");
    label.className = "variant-nav-label"; label.textContent = `${index + 1}/${total}`;
    label.title = msg.role === "user" ? "Edited versions of this message" : "Alternative replies";
    const next = document.createElement("button");
    next.type = "button"; next.className = "variant-nav-btn"; next.textContent = "▶";
    next.setAttribute("aria-label", "Next variant"); next.disabled = index === total - 1;
    next.addEventListener("click", step(1));
    nav.append(prev, label, next);
    return nav;
  }

  function messageActions(msg, el) {
    const bar = document.createElement("div");
    bar.className = "message-actions";
    if (msg.variants?.length > 1) bar.append(variantNav(msg));
    const copy = actionButton("Copy", "btn-copy", "Copy this message", () => copyText(msg.content, copy));
    bar.append(copy);
    bar.append(actionButton("Edit", "btn-edit", "Save an edited copy as a new variant", () => startEdit(msg, el)));
    if (msg.role === "assistant" && msg.parent_id) {
      bar.append(actionButton("Reroll", "btn-reroll", "Generate another reply to the same message", () => regenerate(msg.parent_id)));
    }
    bar.append(actionButton("Fork", "btn-fork", "Start a new thread from here", () => mutate(async () => {
      const forked = await api.forkMessage(conversationId, msg.id);
      callbacks.onForked?.(forked.id);
    })));
    bar.append(actionButton("Delete", "btn-delete", "Delete this message and the replies below it", () => {
      if (!confirm("Delete this message and every reply below it? Other variants of this message are kept.")) return;
      mutate(() => api.deleteMessage(conversationId, msg.id, revision));
    }));
    return bar;
  }

  function startEdit(msg, el) {
    if (streaming) return;
    editing = msg.id;
    const content = el.querySelector(".message-content");
    const area = document.createElement("textarea");
    area.className = "edit-textarea";
    area.value = msg.content;
    area.setAttribute("aria-label", "Edit message");
    const actions = document.createElement("div");
    actions.className = "edit-actions";
    const done = () => { editing = null; renderThread(); };
    const save = async (thenRegenerate) => {
      const text = area.value.trim();
      if (!text) { alert("A message cannot be empty."); return; }
      if (text === msg.content) { done(); return; }
      let created;
      try {
        created = await api.editMessage(conversationId, msg.id, text, revision);
      } catch (err) {
        alert(err.status === 409 ? `${err.message}. The thread was reloaded.` : err.message);
        editing = null;
        try { await reload(); } catch { /* ignore */ }
        return;
      }
      editing = null;
      await reload().catch(() => {});
      if (thenRegenerate) await regenerate(created.id);
    };
    const saveBtn = actionButton("Save", "btn-edit", "Keep the original and save this as a new variant", () => save(false));
    actions.append(saveBtn);
    if (msg.role === "user") actions.append(actionButton("Save & reply", "btn-reroll", "Save as a new variant and generate a reply", () => save(true)));
    actions.append(actionButton("Cancel", "", "Discard changes", done));
    area.addEventListener("keydown", (event) => {
      if (event.key === "Escape") { event.preventDefault(); done(); }
    });
    content.replaceChildren(area, actions);
    area.focus();
  }

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
    markDialog(content);
    el.appendChild(content);

    if (msg.status && msg.status !== "complete") {
      const badge = document.createElement("span");
      badge.className = "status-badge";
      badge.textContent = msg.status;
      el.appendChild(badge);
    }
    if (msg.origin === "edit" || msg.origin === "reroll" || msg.origin === "fork") {
      const badge = document.createElement("span");
      badge.className = "status-badge origin-badge";
      badge.textContent = { edit: "edited", reroll: "reroll", fork: "forked" }[msg.origin];
      el.appendChild(badge);
    }
    decorateSpeaker(el, msg.speaker, msg.role);
    if (msg.id && msg.status !== "streaming" && !streaming) el.appendChild(messageActions(msg, el));
    return el;
  }

  function renderThread(list = messages) {
    thread.innerHTML = "";
    thread.classList.toggle("busy", streaming);
    for (const msg of list) {
      thread.appendChild(renderMessageNode(msg));
      if (msg.id === editing) startEdit(msg, thread.lastChild);
    }
    thread.scrollTop = thread.scrollHeight;
  }

  renderThread();
  const castControls = await mountCastControls(castSlot, conversationId, async () => {
    await reload();
  }, (character, persona) => {
    activeCharacter = character; activePersona = persona;
    background.hidden = !character?.background;
    if (character?.background) background.src = character.background; else background.removeAttribute("src");
  }, lifecycle.signal, conv.messages.length === 0);


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

  // generate streams one reply. body is either {content} (a new user turn appended to the
  // visible branch) or {regenerate_parent_id} (a new reply variant to that user message).
  async function generate(body, confirmLoad) {
    sendBtn.disabled = true;
    streaming = true;
    castControls.setBusy(true);
    const modelSelect = modelSlot.querySelector("select");
    if (modelSelect) modelSelect.disabled = true;
    stopBtn.hidden = false;
    abortController = new AbortController();

    const regenerating = body.regenerate_parent_id !== undefined;
    let shown = messages;
    if (regenerating) {
      const at = messages.findIndex((m) => m.id === body.regenerate_parent_id);
      shown = at >= 0 ? messages.slice(0, at + 1) : messages;
    }
    renderThread(shown);
    let userEl = null;
    if (!regenerating) {
      userEl = renderMessageNode({ role: "user", content: body.content, status: "complete", speaker: activePersona });
      thread.appendChild(userEl);
    }
    const assistantMsg = { role: "assistant", content: "", reasoning: "", status: "streaming", speaker: activeCharacter };
    let assistantEl = renderMessageNode(assistantMsg);
    thread.appendChild(assistantEl);
    thread.scrollTop = thread.scrollHeight;

    try {
      await streamGenerate(
        conversationId,
        { ...body, revision, confirm_load: confirmLoad },
        (ev) => {
          if (ev.generation_id) currentGenerationId = ev.generation_id;
          if (ev.kind === "start") {
            api.getConversation(conversationId).then(fresh => {
              const last = fresh.messages.at(-1);
              if (last) {
                assistantMsg.speaker = last.speaker;
                decorateSpeaker(assistantEl, assistantMsg.speaker, "assistant");
              }
              const before = fresh.messages.at(-2);
              if (userEl && before) decorateSpeaker(userEl, before.speaker, "user");
            }).catch(() => {});
          }
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
        const proceed = confirm(
          `Model "${currentModelId}" is not loaded. Loading it now may take a while and could evict ` +
            "another client's model from memory. Load it and continue?",
        );
        if (proceed) {
          streaming = false;
          sendBtn.disabled = false;
          stopBtn.hidden = true;
          await generate(body, true);
          return;
        }
      } else if (err.name !== "AbortError") {
        alert(err.status === 409 ? `${err.message}. The thread was reloaded.` : `Generation failed: ${err.message}`);
      }
    } finally {
      streaming = false;
      sendBtn.disabled = !currentModelId;
      castControls.setBusy(false);
      if (modelSelect) modelSelect.disabled = false;
      stopBtn.hidden = true;
      currentGenerationId = null;
      try { await reload(); } catch {
        // The conversation may have just been deleted from elsewhere;
        // leave the last rendered state as is.
      }
    }
  }

  function regenerate(parentId) {
    if (streaming || sendBtn.disabled) {
      if (!streaming) alert("Choose a model first.");
      return Promise.resolve();
    }
    return generate({ regenerate_parent_id: parentId }, false);
  }

  composer.addEventListener("submit", (e) => {
    e.preventDefault();
    if (sendBtn.disabled) return;
    const content = textarea.value.trim();
    if (!content) return;
    textarea.value = "";
    generate({ content }, false);
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
