const statusEl = document.getElementById("status");

async function checkHealth() {
  try {
    const res = await fetch("/api/health", {
      headers: { "X-Labbonet": "1" },
    });
    if (!res.ok) {
      throw new Error(`backend returned ${res.status}`);
    }
    const data = await res.json();
    statusEl.textContent = `Backend ${data.status} (version ${data.version})`;
    statusEl.classList.add("ok");
  } catch (err) {
    statusEl.textContent = `Backend unreachable: ${err.message}. Reopen the launch URL printed at startup.`;
    statusEl.classList.add("error");
  }
}

checkHealth();
