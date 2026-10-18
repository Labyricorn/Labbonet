import { mountCastLibrary } from "./js/cast.js";
import { api } from "./js/api.js";
import { mountChatView } from "./js/conversations.js";
import { mountEndpointsView } from "./js/endpoints.js";
import { mountDiagnosticsView } from "./js/diagnostics.js";

const statusEl = document.getElementById("status");

async function checkHealth() {
  try {
    const data = await api.health();
    statusEl.textContent = data.status === "ok" ? "ONLINE" : data.status;
    statusEl.title = `Labbonet ${data.version}`;
    document.getElementById("status-dot").classList.add("connected");
    statusEl.classList.add("ok");
    statusEl.classList.remove("error");
  } catch (err) {
    statusEl.textContent = `Backend unreachable: ${err.message}`;
    statusEl.classList.add("error");
    statusEl.classList.remove("ok");
    document.getElementById("status-dot").classList.remove("connected");
  }
}

function switchView(name) {
  document.getElementById("about-dialog").close();
  for (const el of document.querySelectorAll(".view")) {
    el.hidden = el.id !== `view-${name}`;
  }
  for (const btn of document.querySelectorAll("[data-view]")) {
    btn.classList.toggle("active", btn.dataset.view === name);
  }
}

const aboutDialog = document.getElementById("about-dialog");
document.getElementById("about-button").addEventListener("click", () => aboutDialog.showModal());
document.getElementById("close-about").addEventListener("click", () => aboutDialog.close());
document.getElementById("backup-button").addEventListener("click", async () => {
  try {
    const result = await api.backup();
    alert(`Backup created: ${result.path || JSON.stringify(result)}`);
  } catch (err) { alert(`Backup failed: ${err.message}`); }
});

for (const btn of document.querySelectorAll("[data-view]")) {
  btn.addEventListener("click", () => switchView(btn.dataset.view));
}

const sidebar = document.getElementById("sidebar");
const overlay = document.getElementById("sidebar-overlay");
function toggleSidebar(forceClosed = false) {
  const collapsed = forceClosed || !sidebar.classList.contains("collapsed");
  sidebar.classList.toggle("collapsed", collapsed);
  overlay.classList.toggle("open", !collapsed && matchMedia("(max-width: 700px)").matches);
}
document.querySelector(".toggle-sidebar").addEventListener("click", () => toggleSidebar());
document.querySelector(".sidebar-close-btn").addEventListener("click", () => toggleSidebar(true));
overlay.addEventListener("click", () => toggleSidebar(true));
if (matchMedia("(max-width: 700px)").matches) toggleSidebar(true);

mountCastLibrary(document.getElementById("cast-button"));
checkHealth();
mountChatView(document.getElementById("view-chat"));
mountEndpointsView(document.getElementById("view-endpoints"));
mountDiagnosticsView(document.getElementById("view-diagnostics"));
switchView("chat");
