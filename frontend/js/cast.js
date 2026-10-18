// Copyright (C) 2026 Elodine / GoblinCorps. AGPL-3.0-or-later.
// CAST structure adapted from GobboNet chat.html @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f.
// Modified by the Labbonet project, 2026-10-18: safe DOM, revisioned backend,
// local raster uploads, inert templates and supported provider parameters only.
// Labbonet additions/modifications: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Upstream credit covers presentation structure/design; runtime/API/state/security code is Labbonet work.
// Element-by-element source and authorship boundary: docs/REUSE-LOG.md and root NOTICE.md.
import { api, dataApi } from "./api.js";
import { showConfirm } from "./dialogs.js";

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
function imagePreview(container, value, name = "--") {
  container.replaceChildren();
  if (/^data:image\/(png|jpeg);base64,/.test(value || "")) {
    const img = node("img"); img.src = value; img.alt = name; container.append(img);
  } else container.textContent = name.slice(0, 2);
}

// Only validated hex colors enter CSSOM. No style attributes, raw stylesheets,
// arbitrary selectors, URLs, or remote images are accepted from cards.
const colors = new Map();
const sheet = new CSSStyleSheet();
document.adoptedStyleSheets = [...document.adoptedStyleSheets, sheet];
export function decorateSpeaker(el, speaker, role) {
  const name = speaker?.name || (role === "user" ? "You" : "Assistant");
  el.querySelector(".message-role").textContent = name;
  imagePreview(el.querySelector(".msg-avatar"), speaker?.portrait, name);
  for (const cls of [...el.classList]) if (cls.startsWith("cast-color-")) el.classList.remove(cls);
  const text = /^#[\da-f]{6}$/i.test(speaker?.text_color || "") ? speaker.text_color : "inherit";
  const dialog = /^#[\da-f]{6}$/i.test(speaker?.dialog_color || "") ? speaker.dialog_color : text;
  const key = `${text}/${dialog}`;
  if (!colors.has(key) && colors.size < 4096) {
    const cls = `cast-color-${colors.size}`; colors.set(key, cls);
    sheet.insertRule(`.${cls} .message-content { color: ${text}; }`);
    sheet.insertRule(`.${cls} .dialog-text { color: ${dialog}; }`);
  }
  if (colors.has(key)) el.classList.add(colors.get(key));
}

export function markDialog(content) {
  const walker = document.createTreeWalker(content, NodeFilter.SHOW_TEXT);
  const texts = []; while (walker.nextNode()) texts.push(walker.currentNode);
  for (const text of texts) {
    if (text.parentElement?.closest("pre,code,math")) continue;
    const matches = [...text.data.matchAll(/"[^"\n]+"|“[^”\n]+”/g)];
    if (!matches.length) continue;
    const fragment = document.createDocumentFragment(); let start = 0;
    for (const match of matches) {
      fragment.append(document.createTextNode(text.data.slice(start, match.index)));
      fragment.append(node("span", "dialog-text", match[0])); start = match.index + match[0].length;
    }
    fragment.append(document.createTextNode(text.data.slice(start))); text.replaceWith(fragment);
  }
}

export function mountCastLibrary(trigger) {
  const dialog = node("dialog", "modal cast-modal"); dialog.id = "cast-dialog";
  dialog.setAttribute("aria-label", "Cast library"); document.body.append(dialog);
  let cards = [], defaults, draft = null, dirty = false, busy = false;
  const error = node("p", "form-error"); error.setAttribute("role", "alert");
  const notice = node("p", "form-hint", "Select the default cast for new threads. Existing threads keep their own selection.");
  const body = node("div"); const title = node("h2", "", "CAST");
  const close = button("CLOSE", async () => { if (await canLeave()) dialog.close(); });
  dialog.append(title, error, notice, body, node("div", "modal-actions"));
  dialog.lastChild.append(close);
  async function canLeave() { return !busy && (!dirty || await showConfirm("Discard unsaved card changes?", {title:"UNSAVED CARD",confirmText:"DISCARD",cancelText:"KEEP EDITING"})); }
  dialog.addEventListener("close", () => { dirty = false; draft = null; });
  dialog.addEventListener("cancel", async e => { e.preventDefault(); if (await canLeave()) dialog.close(); });
  window.addEventListener("beforeunload", e => { if (dirty) { e.preventDefault(); e.returnValue = ""; } });
  async function perform(action) {
    if (busy) return; busy = true; error.textContent = "";
    dialog.setAttribute("aria-busy", "true");
    try { await action(); } catch (err) { error.textContent = err.message; error.scrollIntoView({block: "nearest"}); }
    finally { busy = false; dialog.removeAttribute("aria-busy"); }
  }
  async function refresh() {
    [cards, defaults] = await Promise.all([api.listCards(), api.getCast()]);
    draft = null; dirty = false; renderLibrary();
    document.dispatchEvent(new Event("cast-changed"));
  }
  function renderLibrary() {
    body.replaceChildren(); notice.hidden = false; title.textContent = "CAST";
    for (const kind of ["character", "persona"]) {
      const heading = node("div", `cast-section-header${kind === "persona" ? " cast-section-header-persona" : ""}`);
      heading.append(node("span", "cast-section-title", kind === "character" ? "⚨ CHARACTERS" : "♙ PERSONAS"),
        node("span", "cast-section-sub", kind === "character" ? "who you are talking to" : "who you are"));
      const grid = node("div", `card-grid${kind === "persona" ? " card-grid-persona" : ""}`);
      const selectionKey = `${kind}_id`; const list = cards.filter(c => c.kind === kind);
      const none = button("No default", () => perform(async () => { defaults = await api.saveCast(0, { ...defaults, [selectionKey]: null }); await refresh(); }));
      grid.append(none);
      for (const [i, card] of list.entries()) {
        const row = node("div", `card-item${defaults[selectionKey] === card.id ? " active" : ""}`);
        const moves = node("div", "card-move");
        for (const [label, delta] of [["▲", -1], ["▼", 1]]) {
          const move = button(label, () => perform(async () => {
            const order = [...list]; [order[i], order[i + delta]] = [order[i + delta], order[i]];
            await api.reorderCards(kind, order); await refresh();
          }), "card-move-btn");
          move.disabled = i + delta < 0 || i + delta >= list.length;
          move.setAttribute("aria-label", `Move ${card.name} ${delta < 0 ? "up" : "down"}`); moves.append(move);
        }
        const avatar = node("div", "card-avatar"); imagePreview(avatar, card.portrait, card.name);
        const info = button("", () => perform(async () => { defaults = await api.saveCast(0, { ...defaults, [selectionKey]: card.id }); await refresh(); }), "card-info card-select");
        info.setAttribute("aria-label", `Use ${card.name} as default ${kind}`);
        info.append(node("div", "card-name", card.name), node("div", "card-desc", card.description || "No description"));
        const actions = node("div", "card-actions");
        const exportMenu = node("select"); exportMenu.setAttribute("aria-label", `Export ${card.name}`);
        for (const [value, text] of [["", "Export…"], ["png", "PNG (V3 + V2)"], ["zip", "CHARX / ZIP"], ["v3", "V3 JSON"], ["v2", "V2 JSON"], ["native", "Labbonet JSON"]]) {
          const option = node("option", "", text); option.value = value; exportMenu.append(option);
        }
        exportMenu.addEventListener("change", () => {
          const format = exportMenu.value; exportMenu.value = "";
          if (format) perform(async () => { await dataApi.download(dataApi.exportCardPath(card.id, format)); });
        });
        actions.append(button("Edit", () => { if (!busy) edit(card); }), exportMenu, button("Copy", () => perform(async () => {
          await api.saveCard({ ...card, id: 0, revision: 0, name: `${card.name.slice(0, 150)} copy`, position: list.length }); await refresh();
        })), button("Del", async () => {
          if (await showConfirm(`Delete ${card.name}? Historical speakers will be preserved.`,{confirmText:"DELETE"})) perform(async () => { await api.deleteCard(card); await refresh(); });
        }));
        row.append(moves, avatar, info, actions); grid.append(row);
      }
      body.append(heading, grid, button(`+ NEW ${kind.toUpperCase()}`, () => {
        if (!busy) edit({kind, name: "", position: list.length, macros: {}, greetings: [], carousel: [], persona_frequency: 5});
      }, "btn btn-fill btn-block"));
    }
    body.append(button("IMPORT CARDS", () => document.dispatchEvent(new Event("open-data"))), button("RELOAD LIBRARY", () => perform(refresh)));
  }
  function edit(card) {
    draft = structuredClone(card); dirty = false; body.replaceChildren(); notice.hidden = true;
    title.textContent = `${card.id ? "EDIT" : "NEW"} ${card.kind.toUpperCase()}`;
    const form = node("form"); const fields = new Map();
    function field(key, label, type = "text", hint = "") {
      const group = node("div", "form-group"); const id = `cast-${key}`;
      const labelEl = node("label", "", label); labelEl.htmlFor = id;
      const input = node(type === "textarea" ? "textarea" : "input"); input.id = id;
      if (type !== "textarea") input.type = type;
      input.value = draft[key] ?? ""; input.name = key;
      group.append(labelEl, input); if (hint) group.append(node("p", "form-hint", hint));
      form.append(group); fields.set(key, input); return input;
    }
    field("name", `${card.kind} name`).required = true;
    fields.get("name").maxLength = 200;
    for (const key of card.kind === "character" ? ["portrait", "background"] : ["portrait"]) {
      const group = node("div", "form-group"); const label = node("label", "", key === "portrait" ? "Avatar" : "Chat background");
      const preview = node("div", "avatar-preview"); imagePreview(preview, draft[key], draft.name);
      const upload = node("input"); upload.type = "file"; upload.accept = "image/png,image/jpeg";
      upload.setAttribute("aria-label", `Upload ${key}`);
      upload.addEventListener("change", async () => {
        const file = upload.files[0]; if (!file) return;
        if (file.size > 1048576 || !["image/png", "image/jpeg"].includes(file.type)) { error.textContent = "Choose a PNG or JPEG up to 1 MiB."; return; }
        const editing = draft; const reader = new FileReader();
        reader.onload = () => { if (draft !== editing) return; draft[key] = reader.result; dirty = true; imagePreview(preview, draft[key], draft.name); };
        reader.readAsDataURL(file);
      });
      group.append(label, preview, upload, button("Remove image", () => { draft[key] = ""; dirty = true; upload.value = ""; imagePreview(preview, ""); }));
      form.append(group);
    }
    field("text_color", "Text color", "text", "Optional #RRGGBB; blank uses the theme.").pattern = "#[0-9a-fA-F]{6}";
    field("dialog_color", "Quoted dialog color", "text", "Optional #RRGGBB.").pattern = "#[0-9a-fA-F]{6}";
    field("description", "Description", "textarea");
    if (card.kind === "character") {
      for (const [key,label] of [["system_prompt","System prompt"],["style","Writing style"],["personality","Personality"],["lore","Authored lore"]]) field(key,label,"textarea");
      const book = field("storybook", "Storybook (retrieval lore)", "textarea", "Optional. Start an entry with “# name”, then “tags: angel, divine [0.8]”, “relation: other”, “use: guidance”, “offscreen_default: true” and its description. Entries are added to the prompt only when their tags (or, if enabled, semantic similarity) match the recent conversation. A line of --- ends an entry; prose after it is searchable by meaning only.");
      book.rows = 10; book.maxLength = 262144;
      const bookReport = node("div", "storybook-report"); bookReport.setAttribute("aria-live", "polite");
      form.append(button("CHECK STORYBOOK", () => perform(async () => {
        const out = await api.previewStorybook(book.value); bookReport.replaceChildren();
        bookReport.append(node("p", "form-hint", `${out.counts.entities} entities, ${out.counts.subtrees} subtrees, ${out.counts.tags} tags, ${out.prose_chunks} standalone prose chunks (${out.embeddable_docs} embeddable texts)`));
        for (const e of out.entities.slice(0, 60)) bookReport.append(node("div", "data-item", `${e.id} — ${e.tags} tag(s)${e.relations ? `, ${e.relations} relation(s)` : ""}${e.offscreen ? ", offscreen" : ""}${e.chars ? `, ${e.chars} chars` : ", no description"}`));
        for (const w of out.warnings) bookReport.append(node("p", "form-error memory-note", w));
      })), bookReport);
      field("greetings", "Greeting variants", "textarea", "Separate variants with a line containing ---.").value = (draft.greetings || []).join("\n---\n");
      field("carousel", "Carousel prompts", "textarea", "Separate entries with a line containing ---. Use {{carousel}} in templates.").value = (draft.carousel || []).join("\n---\n");
      const mode = field("carousel_mode", "Carousel mode");
      const select = node("select"); select.id = mode.id;
      for (const value of ["sequential", "random"]) { const option = node("option", "", value); option.value = value; select.append(option); }
      select.value = draft.carousel_mode || "sequential"; mode.replaceWith(select); fields.set("carousel_mode", select);
      for (const [key,label,max] of [["context_limit","Context limit (0 = model inventory / fallback)",2000000],["reply_limit","Reply tokens (0 = 256 fallback)",32768],["temperature","Temperature (blank = provider default)",2],["top_p","Top P (blank = provider default)",1]]) {
        const input = field(key,label,"number"); input.min = "0"; input.max = String(max); input.step = key === "temperature" || key === "top_p" ? "0.01" : "1";
      }
      const presets = node("div", "cast-presets");
      for (const [label,t,p] of [["Precise",0.2,0.9],["Balanced",0.7,1],["Creative",1,1],["Provider default","",""]]) presets.append(button(label, () => { fields.get("temperature").value=t; fields.get("top_p").value=p; dirty=true; }));
      form.append(presets, node("p", "form-hint", "XTC, DRY, min-p and token bias are unavailable: these provider contracts are not verified. Model load/unload and executable card hooks are excluded."));
    } else {
      const frequency = field("persona_frequency", "Description injection frequency", "number", "0 disables; otherwise on turn 1 and every Nth user turn, at the prompt tail. Identity remains on every message.");
      frequency.min = "0"; frequency.max = "1000"; frequency.step = "1";
    }
    field("macros", "Custom text macros (JSON object)", "textarea", "{{char}}, {{user}}, {{date}}, {{time}} and {{carousel}} are reserved. Text only; cycles and expansion size are checked before generation.").value = JSON.stringify(draft.macros || {}, null, 2);
    form.addEventListener("input", () => { dirty = true; });
    const actions = node("div", "modal-actions"); const save = node("button", "btn btn-fill", "SAVE"); save.type = "submit";
    actions.append(button("BACK", async () => { if (await canLeave()) { draft = null; dirty = false; error.textContent=""; renderLibrary(); } }), save); form.append(actions);
    form.addEventListener("submit", e => {
      e.preventDefault(); perform(async () => {
        const result = { ...draft };
        for (const [key, input] of fields) {
          if (key === "macros") result[key] = JSON.parse(input.value);
          else if (["greetings", "carousel"].includes(key)) result[key] = input.value.trim() ? input.value.split(/^---\s*$/m).map(s => s.trim()) : [];
          else if (input.type === "number") result[key] = input.value === "" ? (["temperature", "top_p"].includes(key) ? null : 0) : Number(input.value);
          else result[key] = input.value;
        }
        await api.saveCard(result); await refresh();
      });
    });
    body.append(form); fields.get("name").focus();
  }
  trigger.addEventListener("click", () => { dialog.showModal(); perform(refresh); });
}

// Each mounted conversation owns its selection revision; saving after another
// tab changes/deletes a card displays the conflict and reloads, never overwrites.
export async function mountCastControls(container, conversationId, onGreeting, onCast, signal, initiallyEmpty = true) {
  let selection, cards = [], busy = false, empty = initiallyEmpty;
  const controls = node("fieldset", "cast-controls");
  const error = node("p", "form-error"); error.setAttribute("role", "alert");
  const character = node("select"), persona = node("select"), greeting = node("select");
  const greet = button("ADD GREETING", async () => {
    try { setBusy(true); await api.greet(conversationId, selection.revision); empty = false; await onGreeting(); await refresh(); }
    catch (err) { error.textContent = err.message; } finally { setBusy(false); }
  });
  for (const [label,input] of [["Character",character],["Persona",persona],["Greeting",greeting]]) {
    const wrap = node("label", "cast-control-label", label); input.setAttribute("aria-label", label); wrap.append(input); controls.append(wrap);
  }
  container.append(controls, error); controls.append(greet);
  function setBusy(value) { busy = value; controls.disabled = value; }
  function options(select, kind, selected) {
    select.replaceChildren(); const none = node("option", "", kind === "character" ? "Assistant" : "You"); none.value = ""; select.append(none);
    for (const card of cards.filter(c => c.kind === kind)) { const option = node("option", "", card.name); option.value = card.id; select.append(option); }
    select.value = selected || "";
  }
  function render() {
    options(character, "character", selection.character_id); options(persona, "persona", selection.persona_id);
    const card = cards.find(c => c.id === selection.character_id); greeting.replaceChildren();
    for (const [i,text] of (card?.greetings || []).entries()) { const option = node("option", "", `${i + 1}: ${text.slice(0, 50)}`); option.value = i; greeting.append(option); }
    greeting.value = String(selection.greeting); greeting.disabled = !card?.greetings?.length; greet.disabled = greeting.disabled || !empty;
    greet.title = empty ? "Insert the selected greeting into this empty thread" : "Greetings can only be added to empty threads";
    onCast(card, cards.find(c => c.id === selection.persona_id));
  }
  async function refresh() {
    const result = await Promise.all([api.getCast(conversationId), api.listCards()]);
    if (signal.aborted) return; [selection, cards] = result; render();
  }
  for (const input of [character, persona, greeting]) input.addEventListener("change", async () => {
    error.textContent = "";
    const next = { ...selection, character_id: character.value ? Number(character.value) : null, persona_id: persona.value ? Number(persona.value) : null, greeting: input === greeting ? Number(greeting.value) : 0 };
    try { setBusy(true); selection = await api.saveCast(conversationId, next); render(); }
    catch (err) { error.textContent = err.message; await refresh().catch(() => {}); } finally { setBusy(false); }
  });
  document.addEventListener("cast-changed", () => { if (!busy) refresh().catch(err => { error.textContent = err.message; }); }, { signal });
  await refresh(); return { setBusy };
}
