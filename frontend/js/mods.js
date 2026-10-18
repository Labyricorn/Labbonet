// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// MODS dialog: macros, appearance presets and web-search settings. Behavior reference
// (macro list/editor with trigger rules, an extensions panel, a search key and test
// connection): GobboNet chat.html, js/20-macros.js and js/11-search.js @
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f, Copyright (C) 2026 Elodine / GoblinCorps. No
// upstream script is copied. GobboNet's extension loader (script/stylesheet URLs and inline
// code) is intentionally NOT reproduced: appearance presets are allowlisted colours and
// numbers applied through the CSSOM, so nothing here can fetch or run code. See
// docs/REUSE-LOG.md. Every value from the server is set with textContent.
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
const clip = (s, n) => (s.length > n ? `${s.slice(0, n - 1)}…` : s);

// ---- appearance (also used at page load) -------------------------------------------

const COLOR = /^#[0-9a-fA-F]{6}$/;
const NUMBER = /^[0-9]+(\.[0-9]+)?$/;
const VAR_LABELS = {
  "--cyan-bright": "Primary accent", "--cyan-mid": "Primary text", "--cyan-dim": "Primary muted", "--magenta": "Secondary accent",
  "--magenta-dim": "Secondary muted", "--white": "Bright text", "--black": "Background", "--surface": "Surface", "--elevated": "Raised surface",
  "--border": "Borders", "--font-scale": "Text size (0.8–1.4)", "--avatar-scale": "Avatar size (0.5–2)",
};

let appliedNames = [];

// applyAppearance sets exactly the allowlisted custom properties in values and clears the rest.
// The server validates every value; this re-checks the shape before touching the CSSOM, so a
// malformed value can never reach the stylesheet.
export function applyAppearance(values, names) {
  const root = document.documentElement.style;
  const all = new Set([...appliedNames, ...names]);
  for (const name of all) {
    const v = values?.[name];
    const ok = v !== undefined && names.includes(name) && (name.endsWith("-scale") ? NUMBER.test(v) : COLOR.test(v));
    if (ok) root.setProperty(name, v); else root.removeProperty(name);
  }
  appliedNames = names.filter((n) => values?.[n] !== undefined);
}

// bootAppearance applies the saved preset as early as possible; a failure leaves the pinned theme.
export async function bootAppearance() {
  try {
    const a = await api.appearance();
    applyAppearance(a.effective, a.adjustable);
  } catch { /* default theme */ }
}

export function mountMods(trigger) {
  const dialog = node("dialog", "modal cast-modal memory-modal mods-modal"); dialog.id = "mods-dialog";
  dialog.setAttribute("aria-label", "Mods");
  document.body.append(dialog);
  const tabs = node("div", "memory-tabs"); tabs.setAttribute("role", "tablist");
  const error = node("p", "form-error"); error.setAttribute("role", "alert");
  const status = node("p", "form-hint"); status.setAttribute("role", "status");
  const body = node("div", "memory-body");
  const actions = node("div", "modal-actions");
  actions.append(button("CLOSE", () => dialog.close()));
  dialog.append(node("h2", "", "MODS"), tabs, error, status, body, actions);

  let tab = "macros", busy = false, appearanceState = null;
  const tabButtons = {};
  for (const [key, label] of [["macros", "MACROS"], ["appearance", "APPEARANCE"], ["search", "SEARCH"], ["about", "EXTENSIONS"]]) {
    const b = button(label, () => show(key), "btn btn-sm memory-tab");
    b.setAttribute("role", "tab"); tabs.append(b); tabButtons[key] = b;
  }

  async function perform(action) {
    if (busy) return; busy = true; error.textContent = ""; dialog.setAttribute("aria-busy", "true");
    try { await action(); } catch (err) { error.textContent = err.message; }
    finally { busy = false; dialog.removeAttribute("aria-busy"); }
  }

  async function show(next) {
    // Leaving the appearance tab (or closing) drops any unsaved preview.
    if (appearanceState && tab === "appearance" && next !== "appearance") revertPreview();
    tab = next; error.textContent = ""; status.textContent = "";
    for (const [key, b] of Object.entries(tabButtons)) { b.classList.toggle("active", key === tab); b.setAttribute("aria-selected", String(key === tab)); }
    body.replaceChildren(node("p", "form-hint", "Loading…"));
    try {
      if (tab === "macros") await renderMacros();
      else if (tab === "appearance") await renderAppearance();
      else if (tab === "search") await renderSearch();
      else await renderAbout();
    } catch (err) { body.replaceChildren(); error.textContent = err.message; }
  }

  // ---- macros ----
  async function renderMacros(editing = null) {
    const data = await api.listMacros();
    body.replaceChildren();
    const list = section("CUSTOM MACROS", `A macro is text you can type as {{name}} in a message, a character or a system prompt. The model receives the expansion; your thread keeps the {{name}} you typed. Names are not case-sensitive. Built-in names cannot be used: ${data.reserved.map((r) => `{{${r}}}`).join(" ")}.`);
    if (!data.macros.length) list.append(node("p", "form-hint", "// no custom macros — add one below"));
    for (const m of data.macros) {
      const row = node("div", "data-item macro-row");
      row.append(node("strong", "macro-trigger", `{{${m.trigger}}}`), node("span", "macro-preview", ` ${clip(m.text.replace(/\s+/g, " "), 64)}`));
      row.append(button("EDIT", () => renderMacros(m)));
      let timer = null;
      const del = button("DEL", () => {
        if (!del.dataset.armed) {
          del.dataset.armed = "1"; del.textContent = "CONFIRM?";
          timer = setTimeout(() => { delete del.dataset.armed; del.textContent = "DEL"; }, 6000); return;
        }
        clearTimeout(timer);
        perform(async () => { await api.deleteMacro(m); status.textContent = `Deleted {{${m.trigger}}}.`; await renderMacros(); });
      }, "btn btn-sm btn-danger");
      row.append(del);
      list.append(row);
    }
    body.append(list);

    const form = section(editing ? `EDIT {{${editing.trigger}}}` : "ADD A MACRO", `A name starts with a letter and uses letters, digits and underscores (up to ${data.limits.trigger_length}). Up to ${data.limits.count} macros of ${data.limits.text_bytes / 1024} KiB each. A macro may use other macros; loops and runaway growth are refused.`);
    const name = node("input"); name.type = "text"; name.maxLength = data.limits.trigger_length; name.autocomplete = "off"; name.spellcheck = false;
    name.setAttribute("aria-label", "Macro name"); name.placeholder = "fast_forward";
    name.value = editing?.trigger || ""; name.readOnly = !!editing;
    const nameLabel = node("label", "cast-control-label", "Name"); nameLabel.append(name);
    const text = node("textarea", "memory-text"); text.setAttribute("aria-label", "Expansion text"); text.rows = 5; text.maxLength = data.limits.text_bytes;
    text.value = editing?.text || ""; text.placeholder = "The text this macro expands to";
    const save = button(editing ? "SAVE CHANGES" : "ADD MACRO", () => perform(async () => {
      await api.saveMacro({ id: editing?.id, revision: editing?.revision, trigger: name.value.trim(), text: text.value });
      status.textContent = editing ? "Macro saved." : "Macro added."; await renderMacros();
    }), "btn btn-fill btn-sm");
    form.append(nameLabel, text, save);
    if (editing) form.append(button("CANCEL", () => renderMacros()));
    body.append(form);

    const test = section("TRY IT", "Expands text with your saved macros and sample built-ins. Nothing is sent to a model.");
    const sample = node("textarea", "memory-text"); sample.rows = 2; sample.setAttribute("aria-label", "Text to expand"); sample.placeholder = "Hello {{user}} — {{fast_forward}}";
    const out = node("pre", "msg-source macro-output"); out.setAttribute("aria-live", "polite");
    test.append(sample, button("EXPAND", () => perform(async () => { out.textContent = (await api.previewMacros(sample.value)).text; })), out);
    body.append(test);
  }

  // ---- appearance ----
  function revertPreview() {
    applyAppearance(appearanceState.effective, appearanceState.adjustable);
  }
  const sameVars = (a, b) => Object.keys(a).length === Object.keys(b).length && Object.keys(a).every((k) => a[k] === b[k]);
  const defaultColor = (name) => {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return COLOR.test(v) ? v.toLowerCase() : "#000000";
  };

  async function renderAppearance(selectedId) {
    const ap = await api.appearance();
    appearanceState = ap;
    body.replaceChildren();
    const all = [...ap.builtin, ...ap.presets];
    const wanted = selectedId ?? ap.active;
    const chosen = all.find((p) => p.id === (wanted || "default")) || all[0];

    const pick = section("PRESET", "Presets change colours and sizes only. Pick one to preview it here; APPLY saves it as your theme.");
    const select = node("select"); select.setAttribute("aria-label", "Preset");
    for (const p of all) { const o = node("option", "", `${p.name}${p.builtin ? "" : " (custom)"}`); o.value = p.id; select.append(o); }
    select.value = chosen.id;
    const editorSlot = node("div", "mods-editor");
    select.addEventListener("change", () => drawEditor(editorSlot, ap, all.find((p) => p.id === select.value)));
    pick.append(select);
    body.append(pick, editorSlot);
    drawEditor(editorSlot, ap, chosen);
  }

  function drawEditor(slot, ap, chosen) {
    slot.replaceChildren();
    const editor = section(chosen.builtin ? "CUSTOMISE (SAVES AS A NEW PRESET)" : "EDIT PRESET", "Tick a setting to override it; unticked settings keep the default theme. Changes preview live and are discarded unless saved.");
    const name = node("input"); name.type = "text"; name.maxLength = 40; name.setAttribute("aria-label", "Preset name");
    name.value = chosen.builtin ? `${chosen.name} copy`.slice(0, 40) : chosen.name;
    const nameLabel = node("label", "cast-control-label", "Name"); nameLabel.append(name);
    editor.append(nameLabel);
    const draft = { ...chosen.vars };
    applyAppearance(draft, ap.adjustable);
    for (const v of ap.adjustable) {
      const row = node("label", "cast-control-label appearance-row");
      const on = node("input"); on.type = "checkbox"; on.checked = draft[v] !== undefined; on.setAttribute("aria-label", `Override ${VAR_LABELS[v] || v}`);
      const input = node("input"); input.setAttribute("aria-label", VAR_LABELS[v] || v);
      if (v.endsWith("-scale")) {
        input.type = "number"; input.step = "0.05"; input.min = v === "--font-scale" ? "0.8" : "0.5"; input.max = v === "--font-scale" ? "1.4" : "2";
        input.value = draft[v] ?? "1";
      } else {
        input.type = "color";
        input.value = COLOR.test(draft[v] || "") ? draft[v].toLowerCase() : defaultColor(v);
      }
      input.disabled = !on.checked;
      const sync = () => { if (on.checked) draft[v] = input.value; else delete draft[v]; input.disabled = !on.checked; applyAppearance(draft, ap.adjustable); };
      on.addEventListener("change", sync); input.addEventListener("input", sync);
      row.append(on, ` ${VAR_LABELS[v] || v} `, input);
      editor.append(row);
    }
    const save = (payload) => api.saveAppearance(payload);
    editor.append(button("APPLY THIS THEME", () => perform(async () => {
      let saved;
      if (chosen.builtin && sameVars(draft, chosen.vars)) {
        saved = await save({ revision: ap.revision, active: chosen.id === "default" ? "" : chosen.id, presets: ap.presets });
      } else if (chosen.builtin) {
        const withNew = await save({ revision: ap.revision, active: ap.active, presets: [...ap.presets, { id: "", name: name.value.trim(), vars: { ...draft } }] });
        saved = await save({ revision: withNew.revision, active: withNew.presets[withNew.presets.length - 1].id, presets: withNew.presets });
      } else {
        saved = await save({ revision: ap.revision, active: chosen.id, presets: ap.presets.map((p) => (p.id === chosen.id ? { id: p.id, name: name.value.trim(), vars: { ...draft } } : p)) });
      }
      applyAppearance(saved.effective, saved.adjustable);
      status.textContent = "Theme applied and saved.";
      await renderAppearance(saved.active);
    }), "btn btn-fill btn-sm"));
    if (!chosen.builtin) {
      let timer = null;
      const del = button("DELETE PRESET", () => {
        if (!del.dataset.armed) {
          del.dataset.armed = "1"; del.textContent = "CONFIRM DELETE?";
          timer = setTimeout(() => { delete del.dataset.armed; del.textContent = "DELETE PRESET"; }, 6000); return;
        }
        clearTimeout(timer);
        perform(async () => {
          const saved = await save({ revision: ap.revision, active: ap.active === chosen.id ? "" : ap.active, presets: ap.presets.filter((p) => p.id !== chosen.id) });
          applyAppearance(saved.effective, saved.adjustable); status.textContent = "Preset deleted."; await renderAppearance(saved.active);
        });
      });
      editor.append(del);
    }
    editor.append(button("RESET TO DEFAULT THEME", () => perform(async () => {
      const saved = await save({ revision: ap.revision, active: "", presets: ap.presets });
      applyAppearance(saved.effective, saved.adjustable); status.textContent = "Default theme restored."; await renderAppearance("");
    })));
    slot.append(editor);
  }

  // ---- search ----
  async function renderSearch() {
    const s = await api.searchSettings();
    body.replaceChildren();
    const privacy = section("PRIVACY BOUNDARY", `Web search sends the text of each message you send, while the “web search” box in the chat is ticked, to ${s.service} together with your key. Nothing is sent unless search is enabled here, a key is stored, and the box is ticked. Results are shown under the reply and given to the model as untrusted text; links are never opened for you. Every other part of Labbonet stays local or goes only to the endpoints you registered.`);
    body.append(privacy);

    const box = section("SERVICE", `State: ${s.enabled ? "ENABLED" : "OFF"} · API key: ${s.credential_set ? "stored (protected, never shown again)" : "not set"}.`);
    const enabled = node("label", "cast-control-label", "Allow web search"); const en = node("input"); en.type = "checkbox"; en.checked = s.enabled; en.setAttribute("aria-label", "Allow web search"); enabled.prepend(en);
    const max = node("label", "cast-control-label", "Results per search"); const mx = node("input"); mx.type = "number"; mx.min = "1"; mx.max = "10"; mx.step = "1"; mx.value = s.max_results; mx.setAttribute("aria-label", "Results per search"); max.append(mx);
    const saveSettings = button("SAVE SETTINGS", () => perform(async () => {
      await api.saveSearchSettings({ revision: s.revision, enabled: en.checked, max_results: Number(mx.value) });
      document.dispatchEvent(new Event("search-changed")); status.textContent = "Search settings saved."; await renderSearch();
    }), "btn btn-fill btn-sm");
    box.append(enabled, max, saveSettings);
    body.append(box);

    const key = section("API KEY", `A free key from your account at ${s.service} (Settings → Keys). It is encrypted for this Windows user and never leaves this computer except in requests to ${s.service}.`);
    const input = node("input"); input.type = "password"; input.autocomplete = "off"; input.spellcheck = false; input.maxLength = 512;
    input.setAttribute("aria-label", "Search API key"); input.placeholder = s.credential_set ? "(stored — enter a new key to replace it)" : "paste key";
    const keyLabel = node("label", "cast-control-label", "Key"); keyLabel.append(input);
    key.append(keyLabel,
      button("SAVE KEY", () => perform(async () => {
        if (!input.value.trim()) throw new Error("Enter a key first.");
        await api.setSearchKey(input.value.trim()); input.value = "";
        document.dispatchEvent(new Event("search-changed")); status.textContent = "Key stored."; await renderSearch();
      }), "btn btn-fill btn-sm"),
      button("REMOVE KEY", () => perform(async () => {
        await api.clearSearchKey(); document.dispatchEvent(new Event("search-changed")); status.textContent = "Key removed."; await renderSearch();
      })));
    body.append(key);

    const test = section("TEST CONNECTION", `Sends the fixed phrase “labbonet connection test” (never conversation text) to ${s.service} with the stored key and reports the outcome.`);
    const result = node("p", "memory-line"); result.setAttribute("aria-live", "polite");
    const run = button("TEST", () => perform(async () => {
      result.textContent = "Testing…";
      try { const r = await api.testSearch(); result.textContent = `Working: the service answered with ${r.results} result(s).`; }
      catch (err) { result.textContent = ""; throw err; }
    }));
    run.disabled = !s.credential_set;
    test.append(run, result);
    body.append(test);
  }

  // ---- extensions ----
  async function renderAbout() {
    const [macros, ap] = await Promise.all([api.listMacros(), api.appearance()]);
    body.replaceChildren();
    const active = [...ap.builtin, ...ap.presets].find((p) => p.id === (ap.active || "default"));
    const now = section("CURRENTLY APPLIED");
    now.append(node("p", "memory-line", `${macros.macros.length} custom macro(s) · appearance preset: ${active ? active.name : "Neon (default)"} · no scripts, no stylesheets, no remote resources`));
    body.append(now);
    const no = section("NOT SUPPORTED, ON PURPOSE", "GobboNet can load extension scripts and stylesheets from a URL or pasted code. Labbonet does not: the page runs only its own bundled code under a strict content-security policy, so a script, a stylesheet link, a remote font or image cannot be added. Macros and appearance presets cover the same everyday uses safely. Imported character cards carry no code that is run.");
    body.append(no);
  }

  dialog.addEventListener("close", () => { if (appearanceState) revertPreview(); });
  trigger.addEventListener("click", () => { dialog.showModal(); show(tab); });
}
