// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// DATA menu: backup, export, import preview/commit and scoped reset. The sidebar
// DATA control and the dialog's modal/button presentation follow GobboNet chat.html @
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f (Copyright (C) 2026 Elodine / GoblinCorps)
// as a design reference; no upstream script is copied. See docs/REUSE-LOG.md.
import { api, dataApi } from "./api.js";

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
function select(options, label) {
  const el = node("select"); el.setAttribute("aria-label", label);
  for (const [value, text] of options) { const o = node("option", "", text); o.value = value; el.append(o); }
  return el;
}
function section(title, hint) {
  const wrap = node("fieldset", "cast-controls");
  wrap.append(node("legend", "", title));
  if (hint) wrap.append(node("p", "form-hint", hint));
  return wrap;
}

export function mountDataDialog(trigger) {
  const dialog = node("dialog", "modal cast-modal"); dialog.id = "data-dialog";
  dialog.setAttribute("aria-label", "Data");
  document.body.append(dialog);
  const status = node("p", "form-hint"); status.setAttribute("role", "status");
  const error = node("p", "form-error"); error.setAttribute("role", "alert");
  let busy = false;
  async function perform(action) {
    if (busy) return; busy = true; error.textContent = ""; dialog.setAttribute("aria-busy", "true");
    try { await action(); } catch (err) { error.textContent = err.message; }
    finally { busy = false; dialog.removeAttribute("aria-busy"); }
  }
  const changed = () => { document.dispatchEvent(new Event("cast-changed")); document.dispatchEvent(new Event("data-changed")); };

  const backup = section("BACKUP", "Writes a credential-free copy of the database into the data folder.");
  backup.append(button("CREATE BACKUP", () => perform(async () => {
    const result = await api.backup(); status.textContent = `Backup created: ${result.path}`;
  })));

  const exp = section("EXPORT", "Snapshots contain cards and threads only: never endpoints, keys or provider settings.");
  const scope = select([["full", "Everything (cards + threads)"], ["cards", "Characters and personas only"], ["conversation", "One thread (with its cast)"]], "Export scope");
  const thread = select([], "Thread to export"); thread.hidden = true;
  scope.addEventListener("change", () => { thread.hidden = scope.value !== "conversation"; });
  exp.append(scope, thread, button("EXPORT", () => perform(async () => {
    if (scope.value === "conversation" && !thread.value) throw new Error("Choose a thread to export.");
    const name = await dataApi.download(dataApi.exportDataPath(scope.value, scope.value === "conversation" ? thread.value : 0));
    status.textContent = `Saved ${name}`;
  })));

  const imp = section("IMPORT", "Labbonet snapshots, Tavern V1/V2/V3 cards (JSON or PNG) and CHARX/ZIP cards. Preview first: nothing is written until you commit.");
  const file = node("input"); file.type = "file"; file.accept = ".json,.png,.charx,.zip,application/json,image/png,application/zip";
  file.setAttribute("aria-label", "File to import");
  const policy = select([["add", "Add all (rename name collisions)"], ["skip", "Skip cards that already exist"]], "Merge policy");
  const defaultsLabel = node("label", "cast-control-label", "Apply exported default cast");
  const defaults = node("input"); defaults.type = "checkbox"; defaultsLabel.prepend(defaults);
  const report = node("div", "data-report"); report.setAttribute("aria-live", "polite");
  const commit = button("COMMIT IMPORT", () => runImport("commit"), "btn btn-fill btn-sm"); commit.disabled = true;
  imp.append(file, policy, defaultsLabel, button("PREVIEW", () => runImport("preview")), commit, report);
  for (const el of [file, policy, defaults]) el.addEventListener("change", () => { commit.disabled = true; report.replaceChildren(); });

  function runImport(mode) {
    return perform(async () => {
      const chosen = file.files[0];
      if (!chosen) throw new Error("Choose a file first.");
      commit.disabled = true;
      const out = await dataApi.importFile(chosen, mode, policy.value, defaults.checked);
      report.replaceChildren();
      report.append(node("p", "", `${out.format}${out.committed ? " — imported" : " — preview only, nothing written"}`));
      const r = out.result;
      report.append(node("p", "form-hint", `Cards: ${r.cards_added} new, ${r.cards_renamed} renamed, ${r.cards_skipped} skipped · Threads: ${r.conversations_added} (${r.messages_added} messages)${r.defaults_applied ? " · defaults applied" : ""}`));
      for (const item of out.items) {
        const li = node("div", "data-item");
        li.append(node("strong", "", `${item.kind}: ${item.name}`), node("span", "form-hint", ` ${item.format}`));
        for (const note of item.notes) li.append(node("div", "form-hint", `• ${note}`));
        report.append(li);
      }
      if (out.committed) { status.textContent = "Import complete."; file.value = ""; changed(); }
      else commit.disabled = false;
    });
  }

  const reset = section("RESET", "Removes data permanently after writing a backup. Endpoints and credentials are never removed.");
  const rscope = select([["conversations", "All threads and messages"], ["cast", "All characters and personas"], ["all", "Threads and cast"]], "Reset scope");
  const phrase = node("input"); phrase.type = "text"; phrase.autocomplete = "off"; phrase.setAttribute("aria-label", "Confirmation phrase");
  const hint = node("p", "form-hint");
  const resetBtn = button("RESET", () => perform(async () => {
    const expected = `RESET ${rscope.value.toUpperCase()}`;
    if (phrase.value !== expected) throw new Error(`Type ${expected} exactly to confirm.`);
    if (!confirm(`Permanently delete: ${rscope.options[rscope.selectedIndex].text}? A backup is written first.`)) return;
    const out = await dataApi.reset(rscope.value);
    phrase.value = ""; syncHint(); status.textContent = `Reset complete. Backup: ${out.backup}`; changed();
  }), "btn btn-sm btn-danger");
  function syncHint() {
    const expected = `RESET ${rscope.value.toUpperCase()}`;
    hint.textContent = `Type ${expected} to enable.`; resetBtn.disabled = phrase.value !== expected;
  }
  rscope.addEventListener("change", syncHint); phrase.addEventListener("input", syncHint); syncHint();
  reset.append(rscope, phrase, hint, resetBtn);

  const actions = node("div", "modal-actions"); actions.append(button("CLOSE", () => dialog.close()));
  dialog.append(node("h2", "", "DATA"), error, status, backup, exp, imp, reset, actions);

  trigger.addEventListener("click", async () => {
    status.textContent = ""; error.textContent = ""; dialog.showModal();
    try {
      const list = await api.listConversations();
      thread.replaceChildren(...list.map(c => { const o = node("option", "", c.title || "Untitled"); o.value = c.id; return o; }));
    } catch (err) { error.textContent = err.message; }
  });
  document.addEventListener("open-data", () => { if (!dialog.open) trigger.click(); });
}
