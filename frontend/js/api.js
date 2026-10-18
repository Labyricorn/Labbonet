// Fetch wrapper for the /api backend (ARCHITECTURE.md section 4.1).
// Every request carries the X-Labbonet header the backend's CSRF
// defense requires (SECURITY-MODEL.md section 4); a 401 means the
// session cookie is gone, so every caller is told to reopen the
// launch URL rather than being left looking broken.

const LABBONET_HEADER = "X-Labbonet";

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.name = "ApiError";
    this.status = status;
  }
}

let reauthBannerShown = false;

function showReauthBanner() {
  if (reauthBannerShown) return;
  reauthBannerShown = true;
  const banner = document.createElement("div");
  banner.className = "reauth-banner";
  banner.textContent =
    "Session expired. Reopen the launch URL printed when Labbonet started to continue.";
  document.body.prepend(banner);
}

async function request(method, path, body) {
  const headers = { [LABBONET_HEADER]: "1" };
  if (body !== undefined) headers["Content-Type"] = "application/json";

  const res = await fetch(path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401) {
    showReauthBanner();
    throw new ApiError(401, "Session expired. Reopen the launch URL printed at startup.");
  }
  if (!res.ok) {
    const text = (await res.text().catch(() => "")).trim();
    throw new ApiError(res.status, text || `request failed with status ${res.status}`);
  }
  if (res.status === 204) return null;
  const contentType = res.headers.get("Content-Type") || "";
  if (contentType.includes("application/json")) return res.json();
  return null;
}

export const api = {
  listCards: () => request("GET", "/api/cast"),
  saveCard: (card) => request(card.id ? "PUT" : "POST", card.id ? `/api/cast/${card.id}` : "/api/cast", card),
  deleteCard: (card) => request("DELETE", `/api/cast/${card.id}`, {revision: card.revision}),
  reorderCards: (kind, cards) => request("PUT", "/api/cast/order", {kind, cards: cards.map(({id, revision}) => ({id, revision}))}),
  getCast: (id = 0) => request("GET", id ? `/api/conversations/${id}/cast` : "/api/cast/defaults"),
  saveCast: (id, selection) => request("PUT", id ? `/api/conversations/${id}/cast` : "/api/cast/defaults", selection),
  greet: (id, revision) => request("POST", `/api/conversations/${id}/greeting`, {revision}),

  health: () => request("GET", "/api/health"),

  listEndpoints: () => request("GET", "/api/endpoints"),
  createEndpoint: (body) => request("POST", "/api/endpoints", body),
  patchEndpoint: (id, body) => request("PATCH", `/api/endpoints/${id}`, body),
  deleteEndpoint: (id) => request("DELETE", `/api/endpoints/${id}`),
  setCredential: (id, value) => request("PUT", `/api/endpoints/${id}/credential`, { value }),
  clearCredential: (id) => request("DELETE", `/api/endpoints/${id}/credential`),
  testEndpoint: (id) => request("POST", `/api/endpoints/${id}/test`),
  refreshModels: (id) => request("POST", `/api/endpoints/${id}/models/refresh`),
  listModels: (id) => request("GET", `/api/endpoints/${id}/models`),

  listConversations: () => request("GET", "/api/conversations"),
  createConversation: (body) => request("POST", "/api/conversations", body),
  getConversation: (id) => request("GET", `/api/conversations/${id}`),
  patchConversation: (id, body) => request("PATCH", `/api/conversations/${id}`, body),
  deleteConversation: (id) => request("DELETE", `/api/conversations/${id}`),
  searchConversations: (q) => request("GET", `/api/conversations/search?q=${encodeURIComponent(q)}`),
  organizeConversation: (id, body) => request("PUT", `/api/conversations/${id}/organize`, body),
  reorderConversations: (ids) => request("PUT", "/api/conversations/order", { ids }),
  editMessage: (id, mid, content, revision) => request("POST", `/api/conversations/${id}/messages/${mid}/edit`, { content, revision }),
  activateMessage: (id, mid, revision) => request("POST", `/api/conversations/${id}/messages/${mid}/activate`, { revision }),
  deleteMessage: (id, mid, revision) => request("DELETE", `/api/conversations/${id}/messages/${mid}`, { revision }),
  forkMessage: (id, mid) => request("POST", `/api/conversations/${id}/messages/${mid}/fork`, {}),
  listFolders: () => request("GET", "/api/folders"),
  createFolder: (name) => request("POST", "/api/folders", { name }),
  renameFolder: (id, name) => request("PATCH", `/api/folders/${id}`, { name }),
  deleteFolder: (id) => request("DELETE", `/api/folders/${id}`),

  cancelGeneration: (id) => request("POST", `/api/generations/${id}/cancel`),
  activeGeneration: (id) => request("GET", `/api/conversations/${id}/generation`),
  diagnostics: () => request("GET", "/api/diagnostics"),
  backup: () => request("POST", "/api/backup"),
};

// readEvents decodes an SSE response over fetch. EventSource can't be used: generate needs
// a POST body and both calls need the X-Labbonet header. onEvent is called once per
// decoded "data:" line (each a JSON object per internal/api's sseEventPayload shape); the
// promise resolves once the stream ends, however it ends.
async function readEvents(res, onEvent) {
  if (res.status === 401) {
    showReauthBanner();
    throw new ApiError(401, "Session expired. Reopen the launch URL printed at startup.");
  }
  if (!res.ok || !res.body) {
    const text = (await res.text().catch(() => "")).trim();
    throw new ApiError(res.status, text || `stream failed with status ${res.status}`);
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let sep;
    while ((sep = buffer.indexOf("\n\n")) !== -1) {
      const rawEvent = buffer.slice(0, sep);
      buffer = buffer.slice(sep + 2);
      const dataLine = rawEvent.split("\n").find((l) => l.startsWith("data:"));
      if (!dataLine) continue;
      const json = dataLine.slice(5).trim();
      if (!json) continue;
      onEvent(JSON.parse(json));
    }
  }
}

// streamGenerate starts a reply: POST /api/conversations/{id}/generate. The reply runs as a
// detached job on the server, so aborting this fetch only stops watching it; re-attach with
// attachGeneration. Events: snapshot, start, delta, usage, timings, done, end.
export async function streamGenerate(conversationId, body, onEvent, signal) {
  const res = await fetch(`/api/conversations/${conversationId}/generate`, {
    method: "POST",
    headers: { [LABBONET_HEADER]: "1", "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  });
  await readEvents(res, onEvent);
}

// attachGeneration watches a running (or just finished) reply. The first event is a
// snapshot of the text so far; a 404 means the server no longer holds the job and the
// stored message is the record.
export async function attachGeneration(generationId, onEvent, signal) {
  const res = await fetch(`/api/generations/${generationId}/events`, { headers: { [LABBONET_HEADER]: "1" }, signal });
  await readEvents(res, onEvent);
}

// Binary-safe helpers for DATA (import/export). Requests carry the same
// X-Labbonet header; downloads are saved from a blob so no navigation occurs.
async function raw(method, path, body, contentType) {
  const headers = { [LABBONET_HEADER]: "1" };
  if (contentType) headers["Content-Type"] = contentType;
  const res = await fetch(path, { method, headers, body });
  if (res.status === 401) {
    showReauthBanner();
    throw new ApiError(401, "Session expired. Reopen the launch URL printed at startup.");
  }
  if (!res.ok) {
    const text = (await res.text().catch(() => "")).trim();
    throw new ApiError(res.status, text || `request failed with status ${res.status}`);
  }
  return res;
}

export const dataApi = {
  async download(path) {
    const res = await raw("GET", path);
    const match = /filename="([^"]+)"/.exec(res.headers.get("Content-Disposition") || "");
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = match ? match[1] : "labbonet-export";
    document.body.append(link);
    link.click();
    link.remove();
    setTimeout(() => URL.revokeObjectURL(url), 10000);
    return link.download;
  },
  async importFile(file, mode, policy, defaults) {
    const query = new URLSearchParams({ mode, policy, defaults: defaults ? "1" : "0" });
    const res = await raw("POST", `/api/data/import?${query}`, file, "application/octet-stream");
    return res.json();
  },
  reset: (scope) => request("POST", "/api/data/reset", { scope, confirm: `RESET ${scope.toUpperCase()}` }),
  exportCardPath: (id, format) => `/api/cast/${id}/export?format=${format}`,
  exportDataPath: (scope, id) => `/api/data/export?scope=${scope}${id ? `&id=${id}` : ""}`,
};
