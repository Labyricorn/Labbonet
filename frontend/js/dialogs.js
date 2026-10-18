// Original implementation: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Small modal form helper (text or choice) built on <dialog>. Modal/button presentation uses
// the pinned GobboNet modal classes (see docs/REUSE-LOG.md); no upstream script is copied.
// Native prompt() is avoided so the flow is keyboard- and automation-friendly.

function node(tag, cls, text) {
  const el = document.createElement(tag);
  if (cls) el.className = cls;
  if (text !== undefined) el.textContent = text;
  return el;
}

// showForm resolves with the entered value, or null when cancelled. options turns the
// field into a select of [value, label] pairs. validate may return an error string.
export function showForm({ title, label, value = "", options, hint, confirmText = "SAVE", validate }) {
  return new Promise((resolve) => {
    const dialog = node("dialog", "modal cast-modal form-dialog");
    dialog.setAttribute("aria-label", title);
    const form = node("form");
    form.method = "dialog";
    const field = options ? node("select") : node("input");
    if (options) {
      for (const [v, text] of options) {
        const option = node("option", "", text);
        option.value = v;
        field.append(option);
      }
      field.value = value;
    } else {
      field.type = "text";
      field.autocomplete = "off";
      field.spellcheck = false;
      field.value = value;
    }
    field.setAttribute("aria-label", label);
    const error = node("p", "form-error");
    error.setAttribute("role", "alert");
    const actions = node("div", "modal-actions");
    const cancel = node("button", "btn", "CANCEL");
    cancel.type = "button";
    const ok = node("button", "btn btn-fill", confirmText);
    ok.type = "submit";
    actions.append(cancel, ok);
    form.append(node("h2", "", title), node("label", "cast-control-label", label), field);
    if (hint) form.append(node("p", "form-hint", hint));
    form.append(error, actions);
    dialog.append(form);
    document.body.append(dialog);
    let settled = false;
    const finish = (result) => {
      if (settled) return;
      settled = true;
      dialog.close();
      dialog.remove();
      resolve(result);
    };
    cancel.addEventListener("click", () => finish(null));
    dialog.addEventListener("cancel", (event) => { event.preventDefault(); finish(null); });
    form.addEventListener("submit", (event) => {
      event.preventDefault();
      const message = validate?.(field.value);
      if (message) { error.textContent = message; return; }
      finish(field.value);
    });
    dialog.showModal();
    field.focus();
    if (!options) field.select();
  });
}
