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

  cancelGeneration: (id) => request("POST", `/api/generations/${id}/cancel`),
  diagnostics: () => request("GET", "/api/diagnostics"),
  backup: () => request("POST", "/api/backup"),
};

// streamGenerate reads the SSE response of
// POST /api/conversations/{id}/generate over fetch. EventSource can't
// be used here: it only supports GET with no custom headers or body,
// and this request needs both the X-Labbonet header and a JSON body.
// onEvent is called once per decoded "data:" line (each a JSON object
// per internal/api's sseEventPayload shape); the promise resolves once
// the stream ends, however it ends.
export async function streamGenerate(conversationId, body, onEvent, signal) {
  const res = await fetch(`/api/conversations/${conversationId}/generate`, {
    method: "POST",
    headers: { [LABBONET_HEADER]: "1", "Content-Type": "application/json" },
    body: JSON.stringify(body),
    signal,
  });

  if (res.status === 401) {
    showReauthBanner();
    throw new ApiError(401, "Session expired. Reopen the launch URL printed at startup.");
  }
  if (!res.ok || !res.body) {
    const text = (await res.text().catch(() => "")).trim();
    throw new ApiError(res.status, text || `generate failed with status ${res.status}`);
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
