// Endpoint manager (ARCHITECTURE.md section 3): register, test,
// credential set/clear, privacy class and plain-HTTP warning
// (SECURITY-MODEL.md section 5.1).
import { api } from "./api.js";
import { showConfirm, showNotice } from "./dialogs.js";

export function mountEndpointsView(root) {
  root.innerHTML = "";
  root.classList.add("endpoints-view");

  const heading = document.createElement("h2");
  heading.textContent = "Endpoints";
  const diagnosticsBtn = document.createElement("button");
  diagnosticsBtn.type = "button";
  diagnosticsBtn.className = "btn btn-sm";
  diagnosticsBtn.textContent = "Diagnostics";
  diagnosticsBtn.addEventListener("click", () => document.querySelector('[data-view="diagnostics"]').click());

  const form = document.createElement("form");
  form.className = "endpoint-form";

  const nameInput = document.createElement("input");
  nameInput.placeholder = "Name";
  nameInput.required = true;

  const urlInput = document.createElement("input");
  urlInput.placeholder = "Base URL, e.g. http://127.0.0.1:8080";
  urlInput.required = true;

  const ackLabel = document.createElement("label");
  ackLabel.className = "ack-external";
  const ackCheckbox = document.createElement("input");
  ackCheckbox.type = "checkbox";
  ackLabel.append(ackCheckbox, document.createTextNode(" This is a public/external address"));

  const submitBtn = document.createElement("button");
  submitBtn.type = "submit";
  submitBtn.className = "btn btn-fill";
  submitBtn.textContent = "Register";

  const errorEl = document.createElement("p");
  errorEl.className = "form-error";

  form.append(nameInput, urlInput, ackLabel, submitBtn, errorEl);

  const list = document.createElement("div");
  list.className = "endpoint-list";

  root.append(heading, diagnosticsBtn, form, list);

  async function refresh() {
    let endpoints = [];
    try {
      endpoints = await api.listEndpoints();
    } catch (err) {
      list.innerHTML = "";
      const p = document.createElement("p");
      p.className = "form-error";
      p.textContent = `Could not load endpoints: ${err.message}`;
      list.appendChild(p);
      return;
    }
    list.innerHTML = "";
    for (const ep of endpoints) list.appendChild(renderEndpointRow(ep));
  }

  function renderEndpointRow(ep) {
    const row = document.createElement("div");
    row.className = "endpoint-row";

    const info = document.createElement("div");
    info.className = "endpoint-info";

    const nameEl = document.createElement("strong");
    nameEl.textContent = ep.name;
    const urlEl = document.createElement("span");
    urlEl.className = "endpoint-url";
    urlEl.textContent = ep.base_url;
    const classBadge = document.createElement("span");
    classBadge.className = `privacy-badge privacy-${ep.privacy_class}`;
    classBadge.textContent = ep.privacy_class;

    info.append(nameEl, document.createTextNode(" — "), urlEl, document.createTextNode(" "), classBadge);
    if (ep.detected_service) {
      const detected = document.createElement("span");
      detected.className = "detected-service";
      detected.textContent = ` (${ep.detected_service})`;
      info.appendChild(detected);
    }
    if (ep.credential_set) {
      const credBadge = document.createElement("span");
      credBadge.className = "credential-badge";
      credBadge.textContent = "credential set";
      info.appendChild(credBadge);
    }
    if (ep.base_url.startsWith("http://") && ep.privacy_class !== "local") {
      const warn = document.createElement("p");
      warn.className = "plain-http-warning";
      warn.textContent =
        "Plain HTTP: conversation content, and any credential, crosses the network in clear text.";
      info.appendChild(warn);
    }

    const actions = document.createElement("div");
    actions.className = "endpoint-actions";

    const testBtn = document.createElement("button");
    testBtn.type = "button";
    testBtn.className = "btn btn-sm";
    testBtn.textContent = "Test";
    testBtn.addEventListener("click", async () => {
      testBtn.disabled = true;
      const originalText = testBtn.textContent;
      try {
        const result = await api.testEndpoint(ep.id);
        testBtn.textContent = result.healthy ? `OK (${result.detected_service})` : "Unreachable";
      } catch (err) {
        testBtn.textContent = "Failed";
      } finally {
        setTimeout(() => {
          testBtn.textContent = originalText;
          testBtn.disabled = false;
          refresh();
        }, 1500);
      }
    });

    const refreshModelsBtn = document.createElement("button");
    refreshModelsBtn.type = "button";
    refreshModelsBtn.className = "btn btn-sm";
    refreshModelsBtn.textContent = "Refresh models";
    refreshModelsBtn.addEventListener("click", async () => {
      refreshModelsBtn.disabled = true;
      try {
        await api.refreshModels(ep.id);
      } catch (err) {
        showNotice(`Refreshing models failed: ${err.message}`);
      } finally {
        refreshModelsBtn.disabled = false;
      }
    });

    const credBtn = document.createElement("button");
    credBtn.type = "button";
    credBtn.className = "btn btn-sm";
    credBtn.textContent = ep.credential_set ? "Clear credential" : "Set credential";
    credBtn.addEventListener("click", async () => {
      try {
        if (ep.credential_set) {
          await api.clearCredential(ep.id);
        } else {
          let value;
          try {
            value = prompt(`Enter the API key or token for "${ep.name}":`);
          } catch {
            // Some embedding contexts disable prompt(); fail quietly
            // rather than leaving an unhandled rejection.
            showNotice("This browser does not allow entering a credential here.");
            return;
          }
          if (!value) return;
          await api.setCredential(ep.id, value);
        }
      } catch (err) {
        showNotice(`Could not update the credential: ${err.message}`);
        return;
      }
      refresh();
    });

    const deleteBtn = document.createElement("button");
    deleteBtn.type = "button";
    deleteBtn.className = "btn btn-danger btn-sm";
    deleteBtn.textContent = "Delete";
    deleteBtn.addEventListener("click", async () => {
      if (!await showConfirm(`Delete endpoint "${ep.name}"? Its credential and model inventory go with it.`,{confirmText:"DELETE"})) return;
      await api.deleteEndpoint(ep.id);
      refresh();
    });

    actions.append(testBtn, refreshModelsBtn, credBtn, deleteBtn);
    row.append(info, actions);
    return row;
  }

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    errorEl.textContent = "";
    try {
      await api.createEndpoint({
        name: nameInput.value,
        base_url: urlInput.value,
        acknowledge_external: ackCheckbox.checked,
      });
      nameInput.value = "";
      urlInput.value = "";
      ackCheckbox.checked = false;
      await refresh();
    } catch (err) {
      errorEl.textContent = err.message;
    }
  });

  refresh();
}
