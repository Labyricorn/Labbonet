import { api } from "./js/api.js";
import { mountChatView } from "./js/conversations.js";
import { mountEndpointsView } from "./js/endpoints.js";
import { mountDiagnosticsView } from "./js/diagnostics.js";

const statusEl = document.getElementById("status");

async function checkHealth() {
  try {
    const data = await api.health();
    statusEl.textContent = `Backend ${data.status} (version ${data.version})`;
    statusEl.classList.add("ok");
    statusEl.classList.remove("error");
  } catch (err) {
    statusEl.textContent = `Backend unreachable: ${err.message}`;
    statusEl.classList.add("error");
    statusEl.classList.remove("ok");
  }
}

function switchView(name) {
  for (const el of document.querySelectorAll(".view")) {
    el.hidden = el.id !== `view-${name}`;
  }
  for (const btn of document.querySelectorAll("nav button")) {
    btn.classList.toggle("active", btn.dataset.view === name);
  }
}

for (const btn of document.querySelectorAll("nav button")) {
  btn.addEventListener("click", () => switchView(btn.dataset.view));
}

checkHealth();
mountChatView(document.getElementById("view-chat"));
mountEndpointsView(document.getElementById("view-endpoints"));
mountDiagnosticsView(document.getElementById("view-diagnostics"));
switchView("chat");
