// Diagnostics panel (ARCHITECTURE.md section 3, proposal section 18):
// endpoint, model, status, timings per recent request. Never shows
// message content; the backend's /api/diagnostics never returns any
// (SECURITY-MODEL.md section 7).
import { api } from "./api.js";

const COLUMNS = [
  "Started",
  "Endpoint",
  "Model requested",
  "Model reported",
  "Status",
  "TTFT",
  "Tokens/s",
  "Duration",
  "Error",
];

export function mountDiagnosticsView(root) {
  root.innerHTML = "";
  root.classList.add("diagnostics-view");

  const heading = document.createElement("h2");
  heading.textContent = "Diagnostics";
  const refreshBtn = document.createElement("button");
  refreshBtn.type = "button";
  refreshBtn.className = "btn btn-sm";
  refreshBtn.textContent = "Refresh";
  const table = document.createElement("table");
  table.className = "diagnostics-table";

  root.append(heading, refreshBtn, table);

  async function refresh() {
    let entries = [];
    try {
      entries = await api.diagnostics();
    } catch (err) {
      table.innerHTML = "";
      const row = document.createElement("tr");
      const cell = document.createElement("td");
      cell.textContent = `Could not load diagnostics: ${err.message}`;
      row.appendChild(cell);
      table.appendChild(row);
      return;
    }

    table.innerHTML = "";
    const header = document.createElement("tr");
    for (const label of COLUMNS) {
      const th = document.createElement("th");
      th.textContent = label;
      header.appendChild(th);
    }
    table.appendChild(header);

    for (const e of entries) {
      const row = document.createElement("tr");
      const ttft = e.timings?.ttft_ms ? `${Math.round(e.timings.ttft_ms)} ms` : "";
      const tps = e.timings?.tokens_per_second ? e.timings.tokens_per_second.toFixed(1) : "";
      const duration = e.duration_ms ? `${Math.round(e.duration_ms)} ms` : "";
      const cells = [
        new Date(e.started_at).toLocaleString(),
        e.endpoint_id != null ? String(e.endpoint_id) : "",
        e.model_requested,
        e.model_reported,
        e.status,
        ttft,
        tps,
        duration,
        e.error_kind,
      ];
      for (const text of cells) {
        const td = document.createElement("td");
        td.textContent = text;
        row.appendChild(td);
      }
      table.appendChild(row);
    }
  }

  refreshBtn.addEventListener("click", refresh);
  refresh();
}
