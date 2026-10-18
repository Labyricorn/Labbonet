// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Web search in the chat view: the composer opt-in toggle and the citations shown under a
// reply. Behavior reference (a per-session search toggle, results saved with the message,
// an indicator for "no key" / "no results"): GobboNet chat.html, 10-chat.js and 11-search.js @
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f, Copyright (C) 2026 Elodine / GoblinCorps. No
// upstream script is copied; see docs/REUSE-LOG.md.
// Everything that came from the search service (titles, links, snippets) is untrusted: it is
// only ever set with textContent, and a link is used only if it parses as http or https.
import { api } from "./api.js";

const PREF_KEY = "labbonet.search.on";

function readPref() {
  try { return localStorage.getItem(PREF_KEY) === "1"; } catch { return false; }
}
function writePref(on) {
  try { localStorage.setItem(PREF_KEY, on ? "1" : "0"); } catch { /* storage unavailable */ }
}

// createSearchToggle builds the per-viewer opt-in. A message is only searched when the box is
// ticked AND the owner has enabled search with a key in MODS → SEARCH; the server enforces the
// second condition regardless of what this control says.
export function createSearchToggle() {
  const label = document.createElement("label");
  label.className = "tool-field search-toggle";
  const box = document.createElement("input");
  box.type = "checkbox";
  box.setAttribute("aria-label", "Search the web for each message I send");
  label.append(box, " web search");
  let ready = false;

  function paint() {
    box.disabled = !ready;
    if (!ready) box.checked = false;
    label.title = ready
      ? "Send each message you type to the web-search service (ollama.com) and give the model the results. Your text leaves this computer."
      : "Web search is off. Turn it on and store a key in MODS → SEARCH.";
    label.classList.toggle("active", box.checked);
  }
  box.addEventListener("change", () => { writePref(box.checked); paint(); });

  async function refresh() {
    try {
      const s = await api.searchSettings();
      ready = !!(s.enabled && s.credential_set);
    } catch {
      ready = false;
    }
    box.checked = ready && readPref();
    paint();
  }
  refresh();
  document.addEventListener("search-changed", refresh);
  return {
    el: label,
    get on() { return box.checked && !box.disabled; },
    refresh,
    dispose() { document.removeEventListener("search-changed", refresh); },
  };
}

// safeLink returns a normalised http(s) URL string or null.
export function safeLink(raw) {
  try {
    const u = new URL(raw);
    return (u.protocol === "https:" || u.protocol === "http:") && !u.username && !u.password ? u : null;
  } catch {
    return null;
  }
}

function node(tag, cls, text) {
  const el = document.createElement(tag);
  if (cls) el.className = cls;
  if (text !== undefined) el.textContent = text;
  return el;
}

// renderSources shows what a reply's web search found (or why it found nothing). record is a
// generate.SearchRecord as stored by the server.
export function renderSources(record) {
  const box = node("details", "web-sources");
  const used = record.used || 0;
  const head = node("summary", "web-sources-head");
  if (record.error) head.textContent = `WEB SEARCH FAILED · ${record.query ? `“${record.query}”` : "no query"}`;
  else head.textContent = `WEB SEARCH · “${record.query}” · ${used} of ${record.results?.length || 0} result(s) given to the model`;
  box.append(head);
  box.classList.toggle("web-sources-failed", !!record.error);
  if (record.error) {
    box.open = true;
    box.append(node("p", "form-error", `${record.error}. The reply was written without search results.`));
    return box;
  }
  if (record.omitted) box.append(node("p", "form-hint", "The results did not fit in the model's context window and were left out."));
  if (!record.results?.length) box.append(node("p", "form-hint", "The service returned no results."));
  const list = node("ol", "web-source-list");
  (record.results || []).forEach((r, i) => {
    const item = node("li", i >= used ? "web-source unused" : "web-source");
    const url = safeLink(r.url);
    if (url) {
      const a = node("a", "", r.title || url.href);
      a.href = url.href;
      a.target = "_blank";
      a.rel = "noopener noreferrer";
      a.referrerPolicy = "no-referrer";
      item.append(a, node("span", "web-source-host", ` ${url.hostname}`));
    } else {
      item.append(node("span", "", r.title || "(untitled)"));
    }
    if (r.content) item.append(node("div", "web-source-snippet", r.content));
    list.append(item);
  });
  box.append(list);
  return box;
}

// loadSources fetches the stored search records for a thread as a Map of assistant message
// id → record. Citations are decoration: a failure just means none are shown.
export async function loadSources(conversationId) {
  const map = new Map();
  try {
    const out = await api.searchRecords(conversationId);
    for (const r of out.records || []) {
      if (r.message_id && !map.has(r.message_id)) map.set(r.message_id, r.record);
    }
  } catch { /* none */ }
  return map;
}
