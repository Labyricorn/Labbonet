// Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Behavior/design references: GobboNet js/13-dashboard.js, js/06-state-sync.js,
// js/21-data.js at 314bdc9cae204ae38fdc1002ac6a9fb1437c109f,
// Elodine / GoblinCorps. No source copied. Safe DOM/API is Labbonet work.
// Profiles are settings bundles in one database; see docs/REUSE-LOG.md.
import { api } from "./api.js";
import { loadPrefs, savePrefs } from "./tools.js";
import { bootAppearance } from "./mods.js";
import { node, button, section, confirmDelete, nextTime } from "./schedule.js";

export function mountHome(panel, signal) {
  const home = node("div", "home-dashboard"); panel.replaceChildren(home);
  const error = node("p", "form-error"); error.setAttribute("role", "alert");
  home.append(error);
  let busy = false, seq = 0, editingProfile = false;
  const notify = (type, detail) => document.dispatchEvent(new CustomEvent(type, { detail }));
  async function perform(action) {
    if (busy) return; busy = true; error.textContent = "";
    try { await action(); }
    catch (err) { if (err.status === 409) await refresh().catch(() => {}); error.textContent = err.message; }
    finally { busy = false; }
  }
  async function refresh() {
    const ticket = ++seq;
    const [cards, threads, schedules, endpoints, profiles] = await Promise.all([
      api.listCards(), api.listConversations(), api.listSchedules(), api.listEndpoints(), api.listProfiles(),
    ]);
    if (signal.aborted || ticket !== seq) return;
    editingProfile = false;
    home.replaceChildren(node("h2", "", "SYSTEMS ONLINE"), error);
    const chars = section("CHARACTERS");
    chars.append(button("NEW THREAD", () => notify("new-thread", {})));
    for (const c of cards.filter(c => c.kind === "character")) chars.append(button(c.name, () => notify("new-thread", { character_id: c.id })));
    if (!cards.some(c => c.kind === "character")) chars.append(node("p", "form-hint", "Add characters in CHARS for quick starts."));
    const recent = section("RECENT THREADS");
    for (const t of [...threads].sort((a, b) => String(b.updated_at).localeCompare(String(a.updated_at))).slice(0, 6)) recent.append(button(t.title || "Untitled", () => notify("open-thread", { id: t.id })));
    if (!threads.length) recent.append(node("p", "form-hint", "No threads yet."));
    const jobs = section("SCHEDULES");
    jobs.append(node("p", "form-hint", `${schedules.status.running ? "Timer active" : "Timer stopped"} · ${schedules.status.active_runs} running. Server must stay running; browser may close.`), button("OPEN SCHED", () => document.getElementById("sched-button").click()));
    for (const s of [...schedules.schedules].sort((a, b) => (a.next_run || "z").localeCompare(b.next_run || "z")).slice(0, 6)) jobs.append(node("p", "memory-line", `${s.name} · ${s.enabled ? nextTime(s) : "Disabled"} · ${s.last_status || "Never run"}`));
    const connections = section("CONNECTIONS");
    for (const e of endpoints) connections.append(node("p", "memory-line", `${e.name} · ${e.privacy_class || e.privacy || ""} · ${e.base_url}`));
    if (!endpoints.length) connections.append(node("p", "form-hint", "No endpoints registered. Add one in CONFIG."));
    connections.append(button("CONFIG", () => document.querySelector('[data-view="endpoints"]').click()));
    home.append(chars, recent, jobs, connections);
    renderProfiles(profiles);
  }
  function renderProfiles(data) {
    const box = section("SETTINGS PROFILES");
    box.append(node("p", "form-hint", "Profiles save theme, optional memory settings and this browser’s chat limits. Threads, characters and connections remain shared in one database. Applied profile labels show the last selection; later edits can differ."));
    const select = node("select"); select.setAttribute("aria-label", "Settings profile");
    const none = node("option", "", "Choose a profile"); none.value = ""; select.append(none);
    for (const p of data.profiles) { const o = node("option", "", p.name); o.value = p.id; select.append(o); }
    select.value = data.active_id || "";
    const label = node("label", "cast-control-label", "Profile name");
    const name = node("input"); name.maxLength = 40; name.setAttribute("aria-label", "Profile name"); label.append(name);
    const memoryLabel = node("label", "cast-control-label", "Include memory settings");
    const memory = node("input"); memory.type = "checkbox"; memory.setAttribute("aria-label", "Include memory settings"); memoryLabel.append(memory);
    const current = () => data.profiles.find(p => p.id === Number(select.value));
    const chosen = () => { const p = current(); name.value = p?.name || ""; memory.checked = !!p?.data.memory; };
    select.addEventListener("change", chosen); chosen();
    const capture = () => { const p = loadPrefs(); return { name: name.value, memory: memory.checked, capture: true, prefs: { reply_cap: p.replyCap, reasoning_limit: p.reasoningLimit, follow: p.follow } }; };
    const needProfile = () => { const p = current(); if (!p) throw new Error("Choose a profile first."); return p; };
    const note = node("p", "form-hint"); note.setAttribute("role", "status");
    box.append(select, label, memoryLabel,
      button("SAVE NEW", () => perform(async () => { await api.saveProfile(capture()); await refresh(); })),
      button("UPDATE FROM CURRENT", () => perform(async () => { const p = needProfile(); await api.saveProfile({ ...capture(), id: p.id, revision: p.revision }); await refresh(); })),
      button("APPLY", () => perform(async () => {
        const p = needProfile(); const result = await api.applyProfile(p.id);
        savePrefs({ replyCap: result.prefs.reply_cap, reasoningLimit: result.prefs.reasoning_limit, follow: result.prefs.follow });
        await bootAppearance(); notify("memory-changed", {}); await refresh();
        // refresh recreates the section, so append feedback to the live dashboard.
        if (!signal.aborted) home.append(node("p", "form-hint", `Applied ${result.applied.join(", ")}. ${result.notes.join(" ")}`));
      })),
      confirmDelete(() => perform(async () => { await api.deleteProfile(needProfile()); await refresh(); })), note);
    home.append(box);
    box.addEventListener("input", () => { editingProfile = true; });
    box.addEventListener("change", () => { editingProfile = true; });
  }
  for (const event of ["schedules-changed", "cast-changed", "data-changed"]) document.addEventListener(event, () => { if (!editingProfile) perform(refresh); }, { signal });
  const timer = setInterval(() => { if (!editingProfile && home.isConnected && !document.hidden && !home.closest("[hidden]") && !home.contains(document.activeElement)) perform(refresh); }, 30000);
  signal.addEventListener("abort", () => clearInterval(timer), { once: true });
  perform(refresh);
}
