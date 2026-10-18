// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// MEMORY dialog: running summary, retrieval inspector and lore/summary/embedding settings.
// Behavior reference (lore indicator, summary editor, retrieval telemetry): GobboNet chat.html,
// 07-prompt.js and 08-rag.js @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f, Copyright (C) 2026
// Elodine / GoblinCorps; no upstream script is copied. The dialog reuses the pinned modal/button
// classes. See docs/REUSE-LOG.md. Every value from the server is set with textContent.
import { api } from "./api.js";

function node(tag, cls, text) {
  const el = document.createElement(tag);
  if (cls) el.className = cls;
  if (text !== undefined) el.textContent = text;
  return el;
}
function button(text, action, cls = "btn btn-sm") {
  const el = node("button", cls, text); el.type = "button";
  el.addEventListener("click", action); return el;
}
function section(title, hint) {
  const wrap = node("fieldset", "cast-controls memory-section");
  wrap.append(node("legend", "", title));
  if (hint) wrap.append(node("p", "form-hint", hint));
  return wrap;
}
const when = (t) => { const d = new Date(t); return Number.isNaN(d.getTime()) ? "" : d.toLocaleString(); };
const clip = (s, n) => (s.length > n ? `${s.slice(0, n)}…` : s);

export function mountMemory(trigger, chip) {
  const dialog = node("dialog", "modal cast-modal memory-modal"); dialog.id = "memory-dialog";
  dialog.setAttribute("aria-label", "Memory");
  document.body.append(dialog);
  const title = node("h2", "", "MEMORY");
  const tabs = node("div", "memory-tabs"); tabs.setAttribute("role", "tablist");
  const error = node("p", "form-error"); error.setAttribute("role", "alert");
  const status = node("p", "form-hint"); status.setAttribute("role", "status");
  const body = node("div", "memory-body");
  const actions = node("div", "modal-actions");
  actions.append(button("CLOSE", () => dialog.close()));
  dialog.append(title, tabs, error, status, body, actions);

  let convId = null, tab = "summary", busy = false;
  let mem = null, settings = null, models = null;

  const tabButtons = {};
  for (const [key, label] of [["summary", "SUMMARY"], ["inspector", "INSPECTOR"], ["settings", "SETTINGS"]]) {
    const b = button(label, () => show(key), "btn btn-sm memory-tab");
    b.setAttribute("role", "tab"); tabs.append(b); tabButtons[key] = b;
  }

  async function perform(action) {
    if (busy) return; busy = true; error.textContent = ""; dialog.setAttribute("aria-busy", "true");
    try { await action(); } catch (err) { error.textContent = err.message; }
    finally { busy = false; dialog.removeAttribute("aria-busy"); }
  }

  async function loadMemory() {
    mem = convId ? await api.conversationMemory(convId) : null;
    updateChip();
  }

  function updateChip() {
    const eff = mem?.summary;
    const lore = mem?.records?.[0]?.record?.injected?.length || 0;
    const parts = [];
    if (eff && (eff.text || eff.through_message_id)) parts.push(`SUMMARY #${eff.seq}${mem.archived_messages ? ` · ${mem.archived_messages} archived` : ""}`);
    if (lore) parts.push(`LORE ${lore}`);
    chip.hidden = !parts.length;
    chip.textContent = parts.join(" · ");
    chip.title = "Open memory: summary, retrieval inspector and settings";
  }

  async function show(next) {
    tab = next; error.textContent = ""; status.textContent = "";
    for (const [key, b] of Object.entries(tabButtons)) { b.classList.toggle("active", key === tab); b.setAttribute("aria-selected", String(key === tab)); }
    body.replaceChildren(node("p", "form-hint", "Loading…"));
    try {
      if (tab === "settings") { settings = await api.memorySettings(); models = await listModels(); await renderSettings(); }
      else { await loadMemory(); if (tab === "summary") renderSummary(); else renderInspector(); }
    } catch (err) { body.replaceChildren(); error.textContent = err.message; }
  }

  // ---- summary ----
  function renderSummary() {
    body.replaceChildren();
    if (!convId) { body.append(node("p", "form-hint", "Open a thread to see its summary. Settings apply to every thread.")); return; }
    const eff = mem.summary;
    const info = section("RUNNING SUMMARY", "Older messages are replaced in the prompt by this text; the messages themselves stay in the thread. A hand edit becomes a new revision and is never overwritten by an automatic pass that started before it.");
    if (eff) {
      info.append(node("p", "memory-line", `Revision #${eff.seq} (${eff.source}${eff.model ? `, ${eff.model}` : ""}) · ${mem.archived_messages} message(s) summarised · ${mem.live_messages} sent verbatim · ${when(eff.created_at)}`));
    } else {
      info.append(node("p", "memory-line", "No summary applies to this branch yet. Enable automatic summaries in SETTINGS, or press SUMMARISE NOW."));
    }
    const text = node("textarea", "memory-text"); text.setAttribute("aria-label", "Summary text");
    text.value = eff?.text || ""; text.rows = 8; text.maxLength = 60000;
    const save = button("SAVE EDIT", () => perform(async () => {
      await api.saveSummary(convId, text.value, mem.latest_seq); status.textContent = "Summary saved as a new revision.";
      await loadMemory(); renderSummary();
    }), "btn btn-fill btn-sm");
    save.disabled = true;
    text.addEventListener("input", () => { save.disabled = text.value === (eff?.text || "") || !text.value.trim(); });
    let confirmTimer = null;
    const clear = button("CLEAR SUMMARY", () => {
      if (!clear.dataset.armed) {
        clear.dataset.armed = "1"; clear.textContent = "CONFIRM CLEAR?";
        confirmTimer = setTimeout(() => { delete clear.dataset.armed; clear.textContent = "CLEAR SUMMARY"; }, 8000); return;
      }
      clearTimeout(confirmTimer);
      perform(async () => { await api.saveSummary(convId, "", mem.latest_seq); status.textContent = "Summary cleared; archived messages return to the prompt."; await loadMemory(); renderSummary(); });
    });
    clear.disabled = !eff?.text;
    const now = button("SUMMARISE NOW", () => perform(async () => {
      status.textContent = "Asking the compression model for a summary…";
      const out = await api.summariseNow(convId);
      status.textContent = (out.notes || []).join(" · ") || "Done.";
      await loadMemory(); renderSummary();
    }));
    const copy = button("COPY", async () => {
      try { await navigator.clipboard.writeText(text.value); status.textContent = "Copied."; }
      catch { status.textContent = "Clipboard unavailable here; select the text and copy it."; }
    });
    info.append(text, save, clear, now, copy);
    body.append(info);

    const hist = section("REVISIONS", "Newest first. Failed passes are listed so gaps are visible; they never change what is summarised.");
    if (!mem.history.length) hist.append(node("p", "form-hint", "None yet."));
    for (const r of mem.history) {
      const row = node("div", `data-item${r.source === "failed" ? " memory-failed" : ""}`);
      row.append(node("strong", "", `#${r.seq} ${r.source}`), node("span", "", ` · ${when(r.created_at)}${r.model ? ` · ${r.model}` : ""}${r.archived_count ? ` · ${r.archived_count} archived` : ""}`));
      row.append(node("div", "form-hint", r.source === "failed" ? r.note : clip(r.text || "(empty — summary cleared)", 220)));
      hist.append(row);
    }
    body.append(hist);
  }

  // ---- inspector ----
  function renderInspector() {
    body.replaceChildren();
    if (!convId) { body.append(node("p", "form-hint", "Open a thread to inspect its retrieval and context decisions.")); return; }
    const records = mem.records || [];
    const wrap = section("RETRIEVAL AND CONTEXT", "One record per generation that used a storybook or summary. Records hold identifiers, scores and costs only, never message or lore text. Cost units are bytes (conservative): 1 unit ≈ 1 byte.");
    if (!records.length) { wrap.append(node("p", "form-hint", "No records yet. Give the character a storybook (CHARS → edit → Storybook) or enable summaries, then send a message.")); body.append(wrap); return; }
    const pick = node("select"); pick.setAttribute("aria-label", "Record");
    records.forEach((r, i) => {
      const o = node("option", "", `${when(r.created_at)} · ${r.record.injected?.length || 0} injected${r.generation_id ? ` · reply #${r.generation_id}` : ""}`);
      o.value = i; pick.append(o);
    });
    const detail = node("div", "memory-detail");
    const draw = () => {
      const r = records[Number(pick.value)].record; detail.replaceChildren();
      const c = r.context || {};
      detail.append(node("p", "memory-line", `Context: ~${c.estimated ?? 0} of ${c.limit ?? 0} units used · summary ${c.summary_seq ? `#${c.summary_seq}` : "none"} · ${c.archived_messages || 0} archived · ${c.omitted_messages || 0} left out · lore ${c.retrieved_cost || 0}`));
      for (const n of r.context_notes || []) detail.append(node("p", "form-error memory-note", n));
      if (r.reason) detail.append(node("p", "form-hint", `Retrieval did not run: ${r.reason}.`));
      const b = r.budget || {};
      detail.append(node("p", "memory-line", `Budget: spent ${b.spent || 0} of ${b.ceiling || 0} (setting ${b.setting || 0}, free in context ${b.free || 0}) · storybook: ${r.storybook?.entities || 0} entities, ${r.storybook?.subtrees || 0} subtrees, ${r.storybook?.tags || 0} tags, ${r.storybook?.prose_chunks || 0} prose`));
      const s = r.semantic || {};
      detail.append(node("p", "memory-line", `Semantic: ${!s.requested ? "off" : s.available ? `on (${s.model}), ${s.docs_scored} docs scored, ${s.new_embeddings} newly embedded${s.pending_docs ? `, ${s.pending_docs} waiting` : ""}` : `unavailable — ${s.reason || "unknown"}; tag retrieval used instead`}`));
      if (r.candidates?.length) {
        const table = node("table", "diagnostics-table memory-table");
        const head = node("tr"); for (const h of ["Entry", "Trigger", "Score", "Result", "Cost"]) head.append(node("th", "", h));
        table.append(head);
        for (const cd of r.candidates) {
          const row = node("tr");
          row.append(node("td", "", cd.id + (cd.matched_tags?.length ? ` (${cd.matched_tags.map(t => `${t.term} ${t.weight}`).join(", ")})` : "")), node("td", "", cd.trigger), node("td", "", String(cd.score)), node("td", "", cd.included ? "injected" : `skipped: ${cd.reason}`), node("td", "", String(cd.cost)));
          table.append(row);
        }
        detail.append(table);
      }
      for (const sh of r.shadow || []) detail.append(node("p", "form-hint", `If the fire threshold were ${sh.fire_threshold}: ${sh.would_fire.length ? sh.would_fire.join(", ") : "nothing"} would fire.`));
    };
    pick.addEventListener("change", draw);
    wrap.append(pick, detail); body.append(wrap); draw();
  }

  // ---- settings ----
  async function listModels() {
    const out = { chat: [], embed: [] };
    let endpoints = []; try { endpoints = await api.listEndpoints(); } catch { /* none */ }
    for (const ep of endpoints) {
      let list = []; try { list = await api.listModels(ep.id); } catch { continue; }
      for (const m of list) {
        const entry = { endpointId: ep.id, endpointName: ep.name, id: m.id, state: m.state };
        if (m.type === "embeddings" || m.embeddings) out.embed.push(entry); else if (m.chat) out.chat.push(entry);
      }
    }
    return out;
  }

  async function renderSettings() {
    body.replaceChildren();
    const s = settings; const inputs = {};
    const num = (key, label, min, max, step, hint) => {
      const wrap = node("label", "cast-control-label", label); const input = node("input"); input.type = "number";
      input.min = min; input.max = max; input.step = step; input.value = s[key]; input.setAttribute("aria-label", label);
      wrap.append(input); if (hint) wrap.title = hint; inputs[key] = input; return wrap;
    };
    const check = (key, label, hint) => {
      const wrap = node("label", "cast-control-label", label); const input = node("input"); input.type = "checkbox"; input.checked = !!s[key];
      input.setAttribute("aria-label", label); wrap.prepend(input); if (hint) wrap.title = hint; inputs[key] = input; return wrap;
    };
    const text = (key, label) => {
      const wrap = node("label", "cast-control-label", label); const input = node("input"); input.type = "text"; input.value = s[key] || ""; input.maxLength = 200;
      input.setAttribute("aria-label", label); wrap.append(input); inputs[key] = input; return wrap;
    };
    const modelSelect = (label, list, endpointKey, modelKey, none) => {
      const wrap = node("label", "cast-control-label", label); const sel = node("select"); sel.setAttribute("aria-label", label);
      const o0 = node("option", "", none); o0.value = ""; sel.append(o0);
      list.forEach((m, i) => { const o = node("option", "", `${m.endpointName}: ${m.id} (${m.state})`); o.value = String(i); sel.append(o); });
      const at = list.findIndex(m => m.endpointId === s[endpointKey] && m.id === s[modelKey]);
      if (at >= 0) sel.value = String(at);
      else if (s[modelKey]) { const o = node("option", "", `${s[modelKey]} (not in inventory)`); o.value = "stale"; sel.append(o); sel.value = "stale"; }
      wrap.append(sel); inputs[modelKey] = sel; return wrap;
    };

    const retrieval = section("LORE RETRIEVAL", "Characters with a storybook get relevant entries injected before your message. Tag retrieval is local and free. Semantic retrieval embeds text with the chosen embedding model.");
    retrieval.append(check("retrieval_enabled", "Retrieval enabled"), check("retriever_b", "Tag retrieval"), check("retriever_a", "Semantic retrieval (needs an embedding model)"),
      num("fire_threshold", "Fire threshold (summed tag weight)", "0.1", "10", "0.1"), num("warmth_turns", "Warmth (extra turns)", "0", "20", "1"),
      num("expansion_depth", "Relation hops (0/1)", "0", "1", "1"), num("top_k_a", "Semantic results", "1", "20", "1"), num("semantic_floor", "Semantic minimum similarity", "0", "1", "0.01"),
      num("retrieval_budget", "Retrieval budget (units)", "0", "100000", "100"), num("window_messages", "Recent messages scanned", "1", "20", "1"));
    const summaries = section("SUMMARIES", "When a thread no longer fits its context window, the oldest messages are summarised instead of dropped. This sends those messages to the compression model (or the thread's own model). It never calls a model that is not loaded.");
    summaries.append(check("summarize_enabled", "Summarise automatically"), num("summary_max_chars", "Summary limit (characters)", "500", "20000", "100"),
      modelSelect("Compression model", models.chat, "compression_endpoint_id", "compression_model", "Same as the thread's model"));
    const embeddings = section("EMBEDDINGS", "Only embedding models listed by an endpoint are offered. Labbonet never loads a model: if the chosen embedding model is not loaded, semantic retrieval is skipped with a reason in the inspector. Prefixes (for example nomic's search_document: / search_query: ) are part of the cached text.");
    embeddings.append(modelSelect("Embedding model", models.embed, "embedding_endpoint_id", "embedding_model", "None"), text("embed_doc_prefix", "Document prefix"), text("embed_query_prefix", "Query prefix"));
    const cache = node("p", "form-hint", "Vector cache: …");
    api.embeddingStats().then(rows => { cache.textContent = rows.length ? `Vector cache: ${rows.map(r => `${r.vectors} on endpoint ${r.endpoint_id} / ${r.model_id}`).join("; ")}` : "Vector cache: empty."; }).catch(() => { cache.textContent = ""; });
    embeddings.append(cache, button("CLEAR VECTOR CACHE", () => perform(async () => { const r = await api.clearEmbeddings(); status.textContent = `Cleared ${r.cleared} cached vector(s).`; cache.textContent = "Vector cache: empty."; })));

    const save = button("SAVE SETTINGS", () => perform(async () => {
      const next = { ...s };
      for (const [key, input] of Object.entries(inputs)) {
        if (input.type === "checkbox") next[key] = input.checked;
        else if (input.type === "number") next[key] = Number(input.value);
        else if (input.tagName === "SELECT") continue;
        else next[key] = input.value;
      }
      for (const [model, endpoint, list] of [["compression_model", "compression_endpoint_id", models.chat], ["embedding_model", "embedding_endpoint_id", models.embed]]) {
        const v = inputs[model].value;
        if (v === "stale") continue;
        if (v === "") { next[model] = ""; next[endpoint] = null; } else { next[model] = list[Number(v)].id; next[endpoint] = list[Number(v)].endpointId; }
      }
      settings = await api.saveMemorySettings(next); status.textContent = "Settings saved."; await renderSettings();
    }), "btn btn-fill btn-sm");
    body.append(retrieval, summaries, embeddings, save);
  }

  trigger.addEventListener("click", () => { dialog.showModal(); show(tab); });
  chip.addEventListener("click", () => { dialog.showModal(); show("summary"); });
  document.addEventListener("conversation-changed", (e) => {
    convId = e.detail?.id ?? null; mem = null;
    if (convId) loadMemory().catch(() => updateChip()); else updateChip();
    if (dialog.open) show(tab);
  });
  document.addEventListener("memory-changed", () => {
    if (!convId) return;
    loadMemory().catch(() => {});
    if (dialog.open && tab !== "settings" && !busy) show(tab);
  });
}
