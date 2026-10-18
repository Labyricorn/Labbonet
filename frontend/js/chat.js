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
import { api, streamGenerate, attachGeneration } from "./api.js";
import { loadPrefs, savePrefs, decorateCode, sourceToggle, addAttachments, createFollow } from "./tools.js";
import { renderMarkdown } from "./render.js";
import { mountModelSelector } from "./models.js";
import { createSearchToggle, renderSources, loadSources } from "./search.js";
import { mountHome } from "./home.js";

let castLifecycle;

export function mountChatPanel(panel) {
  castLifecycle?.abort();
  castLifecycle = new AbortController();
  panel.innerHTML = "";
  mountHome(panel, castLifecycle.signal);
  const title = document.getElementById("thread-title");
  title.value = "No thread selected";
  title.disabled = true;
  document.getElementById("model-slot").replaceChildren();
  document.dispatchEvent(new CustomEvent("conversation-changed", { detail: { id: null } }));
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

  // Chat tools (milestone 4): text-file attachments, reply limits, scroll follow, run status.
  const prefs = loadPrefs();
  const attachments = [];
  const tools = document.createElement("div");
  tools.className = "chat-tools";
  const fileInput = document.createElement("input");
  fileInput.type = "file"; fileInput.multiple = true; fileInput.hidden = true;
  fileInput.setAttribute("aria-label", "Attach text files");
  const attachBtn = actionButton("Attach", "tool-btn", "Attach text files (or drop them on the message box)", () => fileInput.click());
  const chips = document.createElement("span"); chips.className = "attach-chips";
  const limitSelect = (label, title, key, values) => {
    const wrap = document.createElement("label"); wrap.className = "tool-field"; wrap.title = title;
    const select = document.createElement("select");
    for (const value of values) {
      const option = document.createElement("option");
      option.value = String(value); option.textContent = value === 0 ? "off" : value.toLocaleString();
      select.append(option);
    }
    const stored = String(prefs[key]);
    if (![...select.options].some((o) => o.value === stored)) prefs[key] = 0;
    select.value = String(prefs[key]);
    select.addEventListener("change", () => { prefs[key] = Number(select.value); savePrefs(prefs); });
    wrap.append(`${label} `, select);
    return wrap;
  };
  const followLabel = document.createElement("label"); followLabel.className = "tool-field";
  followLabel.title = "Keep the newest text in view while a reply streams; scrolling up pauses it";
  const followBox = document.createElement("input"); followBox.type = "checkbox"; followBox.checked = prefs.follow;
  followBox.addEventListener("change", () => { prefs.follow = followBox.checked; savePrefs(prefs); });
  followLabel.append(followBox, " follow");
  const runStatus = document.createElement("span"); runStatus.className = "gen-status"; runStatus.setAttribute("aria-live", "polite");
  const searchToggle = createSearchToggle();
  tools.append(attachBtn, fileInput, chips, searchToggle.el,
    limitSelect("reply cap", "End a reply at the first sentence end after this many characters", "replyCap", [0, 500, 1000, 2000, 4000]),
    limitSelect("reasoning limit", "Stop a reply whose reasoning grows past this many characters", "reasoningLimit", [0, 2000, 8000, 32000]),
    followLabel, runStatus);

  function renderChips() {
    chips.replaceChildren(...attachments.map((file, index) => {
      const chip = document.createElement("span"); chip.className = "attach-chip";
      const name = document.createElement("span"); name.textContent = `${file.name} (${Math.ceil(file.content.length / 1024)} KB)`;
      const remove = document.createElement("button"); remove.type = "button"; remove.className = "attach-remove";
      remove.textContent = "×"; remove.title = `Remove ${file.name}`; remove.setAttribute("aria-label", `Remove ${file.name}`);
      remove.addEventListener("click", () => { attachments.splice(index, 1); renderChips(); });
      chip.append(name, remove);
      return chip;
    }));
  }
  async function takeFiles(files) {
    const problems = await addAttachments(attachments, [...files]);
    renderChips();
    if (problems.length) alert(problems.join("\n"));
  }
  fileInput.addEventListener("change", () => { takeFiles(fileInput.files).finally(() => { fileInput.value = ""; }); });
  composer.addEventListener("dragover", (e) => { if (e.dataTransfer?.types.includes("Files")) { e.preventDefault(); composer.classList.add("drop-target"); } });
  composer.addEventListener("dragleave", () => composer.classList.remove("drop-target"));
  composer.addEventListener("drop", (e) => {
    if (!e.dataTransfer?.files.length) return;
    e.preventDefault(); composer.classList.remove("drop-target");
    takeFiles(e.dataTransfer.files);
  });

  const castSlot = document.createElement("div"); castSlot.className = "cast-slot";
  const threadWrap = document.createElement("div"); threadWrap.className = "cast-thread-wrap";
  const background = document.createElement("img"); background.className = "cast-background"; background.alt = ""; background.hidden = true;
  const jumpBtn = actionButton("↓ Latest", "jump-bottom", "Jump to the newest message", () => {}); jumpBtn.hidden = true;
  threadWrap.append(background, thread, jumpBtn);
  panel.append(castSlot, threadWrap, tools, composer);
  const follow = createFollow(thread, jumpBtn, () => prefs.follow);

  const conv = await api.getConversation(conversationId);
  titleInput.value = conv.title;
  document.dispatchEvent(new CustomEvent("conversation-changed", { detail: { id: conversationId } }));
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

  // Citations for replies that used web search (message id -> stored search record).
  let sources = new Map();

  async function reload() {
    const fresh = await api.getConversation(conversationId);
    applyConversation(fresh);
    sources = await loadSources(conversationId);
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
    bar.append(sourceToggle(el.querySelector(".message-content"), msg.content, () => renderBody(msg)));
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

  function renderBody(msg) {
    const node = renderMarkdown(msg.content);
    markDialog(node);
    decorateCode(node);
    return node;
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
    content.appendChild(renderBody(msg));
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
    const found = msg.id && sources.get(msg.id);
    if (found) el.appendChild(renderSources(found));
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
    follow?.update(true);
  }

  sources = await loadSources(conversationId);
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
  let timer = null;

  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
  const leaving = () => lifecycle.signal.aborted;
  // Navigating to another thread only stops watching: the reply keeps running on the server
  // and is re-attached when this thread is opened again.
  lifecycle.signal.addEventListener("abort", () => { clearInterval(timer); abortController?.abort(); searchToggle.dispose(); });

  function startTimer(label = "generating") {
    const started = Date.now();
    runStatus.textContent = `${label}… 0s`;
    timer = setInterval(() => { runStatus.textContent = `${label}… ${Math.floor((Date.now() - started) / 1000)}s`; }, 1000);
  }

  function endNote(end) {
    if (!end?.ended) return "";
    if (end.finish === "reply_cap") return "reply cap reached";
    if (end.errorKind === "reasoning_limit") return "reasoning limit reached; reply stopped";
    if (end.status === "cancelled") return "stopped";
    if (end.status === "interrupted") return "interrupted";
    if (end.status === "error") return `error: ${end.errorKind || "failed"}`;
    return "";
  }

  function setBusy(on) {
    streaming = on;
    sendBtn.disabled = on || !currentModelId;
    castControls.setBusy(on);
    const modelSelect = modelSlot.querySelector("select");
    if (modelSelect) modelSelect.disabled = on;
    stopBtn.hidden = !on;
  }

  // The reply being shown while it streams. consume() fills it from either a fresh POST or
  // a re-attached event stream.
  let live = null;
  function refreshLive() {
    const node = renderMessageNode(live.msg);
    live.el.replaceWith(node);
    live.el = node;
    follow.update();
  }

  // consume reads one event stream into the live bubble and reports how it ended. A stream
  // that stops without an "end" event (a dropped connection) leaves ended false so the
  // caller can re-attach.
  async function consume(open) {
    const outcome = { ended: false };
    abortController = new AbortController();
    try {
      await open((ev) => {
        if (ev.generation_id) currentGenerationId = ev.generation_id;
        if (ev.kind === "snapshot") {
          live.msg.content = ev.content || "";
          live.msg.reasoning = ev.reasoning || "";
          refreshLive();
        } else if (ev.kind === "start") {
          api.getConversation(conversationId).then((fresh) => {
            const last = fresh.messages.at(-1);
            if (last && live) {
              live.msg.speaker = last.speaker;
              decorateSpeaker(live.el, live.msg.speaker, "assistant");
            }
            const before = fresh.messages.at(-2);
            if (live?.userEl && before) decorateSpeaker(live.userEl, before.speaker, "user");
          }).catch(() => {});
        } else if (ev.kind === "delta") {
          live.msg.content += ev.content || "";
          live.msg.reasoning += ev.reasoning || "";
          refreshLive();
        } else if (ev.kind === "end") {
          Object.assign(outcome, { ended: true, status: ev.status, errorKind: ev.error_kind, finish: ev.finish_reason });
        }
      }, abortController.signal);
    } catch (err) {
      if (err.name === "AbortError") return outcome;
      throw err;
    }
    return outcome;
  }

  // After a stream stops early, re-attach while the server still reports the reply running.
  async function reattach(outcome) {
    for (let attempt = 0; !outcome.ended && !leaving() && attempt < 30; attempt++) {
      const active = await api.activeGeneration(conversationId).catch(() => null);
      if (!active?.active) break;
      try {
        outcome = await consume((handler, signal) => attachGeneration(active.generation_id, handler, signal));
      } catch {
        await sleep(500);
      }
      if (!outcome.ended) await sleep(300);
    }
    return outcome;
  }

  async function settle(outcome) {
    if (leaving()) return;
    clearInterval(timer);
    runStatus.textContent = endNote(outcome);
    setBusy(false);
    currentGenerationId = null;
    live = null;
    document.dispatchEvent(new Event("memory-changed"));
    try { await reload(); } catch {
      // The conversation may have just been deleted from elsewhere;
      // leave the last rendered state as is.
    }
  }

  // generate starts one reply. body is either {content, attachments} (a new user turn appended to
  // the visible branch) or {regenerate_parent_id} (a new reply variant to that user message).
  async function generate(body, confirmLoad) {
    const searching = searchToggle.on;
    setBusy(true);
    startTimer(searching ? "searching the web, then generating" : "generating");

    const regenerating = body.regenerate_parent_id !== undefined;
    let shown = messages;
    if (regenerating) {
      const at = messages.findIndex((m) => m.id === body.regenerate_parent_id);
      shown = at >= 0 ? messages.slice(0, at + 1) : messages;
    }
    renderThread(shown);
    let userEl = null;
    if (!regenerating) {
      const names = (body.attachments || []).map((a) => `\n\n[Attached file: ${a.name}]`).join("");
      userEl = renderMessageNode({ role: "user", content: body.content + names, status: "complete", speaker: activePersona });
      thread.appendChild(userEl);
    }
    const msg = { role: "assistant", content: "", reasoning: "", status: "streaming", speaker: activeCharacter };
    live = { msg, el: renderMessageNode(msg), userEl };
    thread.appendChild(live.el);
    follow.update(true);

    let outcome = { ended: false };
    try {
      const payload = {
        ...body, revision, confirm_load: confirmLoad,
        max_reply_chars: prefs.replyCap || undefined, reasoning_limit_chars: prefs.reasoningLimit || undefined,
        web_search: searching || undefined,
      };
      outcome = await reattach(await consume((handler, signal) => streamGenerate(conversationId, payload, handler, signal)));
    } catch (err) {
      if (body.attachments?.length) { attachments.unshift(...body.attachments); renderChips(); }
      if (err.status === 409 && /not.loaded/i.test(err.message) && !confirmLoad) {
        const proceed = confirm(
          `Model "${currentModelId}" is not loaded. Loading it now may take a while and could evict ` +
            "another client's model from memory. Load it and continue?",
        );
        if (proceed) {
          clearInterval(timer);
          setBusy(false);
          if (body.attachments?.length) attachments.splice(0);
          renderChips();
          await generate(body, true);
          return;
        }
      } else if (!leaving()) {
        if (err.status === 409 && /^web search/i.test(err.message)) {
          alert(err.message);
          searchToggle.refresh();
        } else {
          alert(err.status === 409 ? `${err.message}. The thread was reloaded.` : `Generation failed: ${err.message}`);
        }
      }
    }
    await settle(outcome);
  }

  // resume re-attaches to a reply that is still running on the server (the tab was closed or
  // the thread was left while it generated).
  async function resume(generationId) {
    const partial = messages.at(-1);
    if (!partial || partial.status !== "streaming") return;
    setBusy(true);
    startTimer();
    renderThread(messages.slice(0, -1));
    live = { msg: { ...partial, content: partial.content || "", reasoning: partial.reasoning || "" }, el: null, userEl: null };
    live.el = renderMessageNode(live.msg);
    thread.appendChild(live.el);
    follow.update(true);
    let outcome = { ended: false };
    try {
      outcome = await consume((handler, signal) => attachGeneration(generationId, handler, signal));
      outcome = await reattach(outcome);
    } catch { /* the stored message is the record; settle reloads it */ }
    await settle(outcome);
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
    const sent = attachments.splice(0);
    renderChips();
    generate(sent.length ? { content, attachments: sent } : { content }, false);
  });

  textarea.addEventListener("keydown", (e) => {
    if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
      e.preventDefault();
      if (!sendBtn.disabled) composer.requestSubmit();
    }
  });

  // A-C4: Stop cancels the running generation through the API (marked "cancelled"). The
  // reply is a detached job, so aborting the browser's fetch alone would not stop it; only
  // when the server does not know the job is the stream abandoned locally.
  stopBtn.addEventListener("click", async () => {
    let cancelled = false;
    if (currentGenerationId) {
      try {
        cancelled = (await api.cancelGeneration(currentGenerationId)).cancelled;
      } catch {
        // Best effort; aborting the watch below still stops the UI.
      }
    }
    if (!cancelled) abortController?.abort();
  });

  const running = await api.activeGeneration(conversationId).catch(() => null);
  if (running?.active && !leaving()) resume(running.generation_id);
}
