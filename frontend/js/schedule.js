// Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior/design reference: GobboNet js/22-scheduler.js at
// 314bdc9cae204ae38fdc1002ac6a9fb1437c109f, Elodine / GoblinCorps.
// No source copied. Safe DOM/API implementation is Labbonet work; docs/REUSE-LOG.md.
import { api } from "./api.js";

export function node(tag, cls = "", text) {
  const el = document.createElement(tag); el.className = cls;
  if (text !== undefined) el.textContent = text;
  return el;
}
export function button(text, action) {
  const el = node("button", "btn btn-sm", text); el.type = "button";
  el.addEventListener("click", action); return el;
}
export function section(title) {
  const el = node("fieldset", "cast-controls memory-section");
  el.append(node("legend", "", title)); return el;
}
export function confirmDelete(action) {
  let until = 0;
  const b = button("DELETE", () => {
    if (Date.now() < until) { until = 0; b.textContent = "DELETE"; action(); return; }
    until = Date.now() + 8000; b.textContent = "CONFIRM DELETE?";
    setTimeout(() => { if (Date.now() >= until) b.textContent = "DELETE"; }, 8100);
  });
  return b;
}
export function nextTime(s) {
  if (!s.next_run) return "No next run";
  const date = new Date(s.next_run);
  const local = date.toLocaleString();
  try { return `${date.toLocaleString(undefined, { timeZone: s.timezone })} ${s.timezone} · local ${local}`; }
  catch { return local; }
}

export function mountSchedule(trigger) {
  const dialog = node("dialog", "modal cast-modal memory-modal"); dialog.id = "schedule-dialog";
  dialog.setAttribute("aria-label", "Schedules");
  const error = node("p", "form-error"); error.setAttribute("role", "alert");
  const status = node("p", "form-hint"); status.setAttribute("role", "status");
  const body = node("div", "memory-body");
  const actions = node("div", "modal-actions");
  actions.append(button("REFRESH", () => perform(refresh)), button("CLOSE", () => dialog.close()));
  dialog.append(node("h2", "", "SCHED"), node("p", "form-hint", "Schedules run only while the Labbonet server is running. The browser may be closed. Missed times are reported, never replayed in a burst. Models must already be available; Labbonet does not load them."), error, status, body, actions);
  document.body.append(dialog);
  let busy = false, selected = null, timer;
  async function perform(action) {
    if (busy) return; busy = true; error.textContent = "";
    dialog.setAttribute("aria-busy", "true");
    try { await action(); }
    catch (err) { if (err.status === 409) await refresh().catch(() => {}); error.textContent = err.message; }
    finally { busy = false; dialog.removeAttribute("aria-busy"); }
  }
  function changed() { document.dispatchEvent(new Event("schedules-changed")); }
  async function refresh() {
    const [data, threads] = await Promise.all([api.listSchedules(), api.listConversations()]);
    body.replaceChildren();
    status.textContent = `${data.status.running ? "Timer active" : "Timer stopped"} · ${data.status.active_runs} running · checks every ${data.status.tick_seconds}s`;
    const list = section("SCHEDULES");
    list.append(button("NEW SCHEDULE", () => { selected = null; renderForm(null, threads); }));
    if (!data.schedules.length) list.append(node("p", "form-hint", "No schedules yet."));
    for (const s of data.schedules) {
      const row = node("div", "data-item schedule-item");
      row.append(node("strong", "", s.name), node("p", "memory-line", `${s.enabled ? "Enabled" : "Disabled"} · ${s.kind} · ${nextTime(s)}`), node("p", "form-hint", `${s.last_status || "Never run"} · ${s.run_count} finished generation attempts (retained ledger)`));
      const controls = node("div", "schedule-actions");
      controls.append(button(s.enabled ? "DISABLE" : "ENABLE", () => perform(async () => { await api.saveSchedule({ ...s, enabled: !s.enabled }); changed(); await refresh(); })),
        button("EDIT", () => { selected = s.id; renderForm(s, threads); }),
        button("DUPLICATE", () => perform(async () => { await api.duplicateSchedule(s.id); changed(); await refresh(); })),
        button("RUN NOW", () => perform(async () => { await api.runSchedule(s.id); selected = s.id; changed(); await refresh(); })),
        button("RUNS", () => perform(async () => { selected = s.id; await refresh(); })),
        confirmDelete(() => perform(async () => { await api.deleteSchedule(s); if (selected === s.id) selected = null; changed(); await refresh(); })));
      row.append(controls); list.append(row);
    }
    body.append(list);
    if (selected && data.schedules.some(s => s.id === selected)) await renderRuns(selected);
  }
  async function renderRuns(id) {
    const runs = await api.scheduleRuns(id);
    const ledger = section("RUN LEDGER (LATEST 50)");
    if (!runs.length) ledger.append(node("p", "form-hint", "No runs yet."));
    for (const r of runs) {
      const row = node("div", "data-item schedule-item");
      row.append(node("strong", "", `${r.status} · ${r.turns} turns · ${r.trigger}`), node("p", "memory-line", new Date(r.started_at).toLocaleString()), node("p", "form-hint", r.reason));
      if (r.conversation_id) row.append(button("OPEN THREAD", () => { dialog.close(); document.dispatchEvent(new CustomEvent("open-thread", { detail: { id: r.conversation_id } })); }));
      if (r.status === "running") row.append(button("STOP RUN", () => perform(async () => { await api.cancelScheduleRun(r.id); changed(); await refresh(); })));
      ledger.append(row);
    }
    body.append(ledger);
  }
  function renderForm(s, threads) {
    body.replaceChildren(); clearInterval(timer);
    const form = node("form", "schedule-form");
    const fields = {};
    function field(key, label, type, value) {
      const wrap = node("label", "cast-control-label", label);
      const input = node(type === "textarea" || type === "select" ? type : "input");
      if (input.tagName === "INPUT") input.type = type;
      input.setAttribute("aria-label", label); fields[key] = input;
      if (type === "checkbox") input.checked = !!value; else input.value = value ?? "";
      wrap.append(input); form.append(wrap); return input;
    }
    const name = field("name", "Schedule name", "text", s?.name); name.required = true; name.maxLength = 80;
    const thread = field("conversation_id", "Thread", "select");
    const placeholder = node("option", "", "Choose a thread"); placeholder.value = ""; thread.append(placeholder);
    for (const t of threads) { const o = node("option", "", t.title || "Untitled"); o.value = t.id; thread.append(o); }
    thread.value = s?.conversation_id ?? ""; thread.required = true;
    const prompt = field("prompt", "Scheduled prompt", "textarea", s?.prompt); prompt.required = true; prompt.rows = 4;
    const kind = field("kind", "Repeat", "select");
    for (const [v, label] of [["once", "Once"], ["daily", "Daily"]]) { const o = node("option", "", label); o.value = v; kind.append(o); }
    kind.value = s?.kind || "once";
    const date = field("date", "Date", "date", s?.date || new Date().toLocaleDateString("en-CA"));
    const clock = field("time", "Time (in selected zone)", "time", s?.time || "09:00"); clock.required = true;
    const zone = field("timezone", "IANA time zone", "text", s?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"); zone.required = true;
    const count = field("auto_continue", "Extra Continue. turns (0–5)", "number", s?.auto_continue || 0); count.min = 0; count.max = 5; count.required = true;
    field("enabled", "Enabled", "checkbox", s?.enabled ?? true);
    field("web_search", "Web search (sends prompt text to Ollama; requires enabled search and key)", "checkbox", s?.web_search);
    const updateKind = () => { date.disabled = kind.value !== "once"; date.required = !date.disabled; }; kind.addEventListener("change", updateKind); updateKind();
    const save = node("button", "btn btn-fill", "SAVE SCHEDULE"); save.type = "submit";
    form.append(save, button("BACK", () => perform(async () => { await refresh(); startPolling(); })));
    form.addEventListener("submit", e => { e.preventDefault(); perform(async () => {
      const data = { id: s?.id, revision: s?.revision };
      for (const [k, el] of Object.entries(fields)) data[k] = el.type === "checkbox" ? el.checked : el.value;
      data.conversation_id = Number(data.conversation_id); data.auto_continue = Number(data.auto_continue);
      await api.saveSchedule(data); changed(); await refresh(); startPolling();
    }); });
    body.append(form); name.focus();
  }
  function startPolling() { clearInterval(timer); timer = setInterval(() => { if (dialog.open) perform(refresh); }, 15000); }
  dialog.addEventListener("close", () => clearInterval(timer));
  trigger.addEventListener("click", () => { dialog.showModal(); selected = null; perform(refresh); startPolling(); });
}
