// Model selector grouped by endpoint (ARCHITECTURE.md section 3,
// proposal section 18): state and context length shown, embeddings
// models hidden from the chat selector (PROVIDER-SPEC.md section 2).
import { api } from "./api.js";

// mountModelSelector renders a <select> into root and calls onChange
// with (endpointId, modelId) whenever the user picks a different
// model. Throws nothing to the caller; a failed endpoint's models are
// simply skipped so one broken endpoint doesn't break the selector.
export async function mountModelSelector(root, currentEndpointId, currentModelId, onChange) {
  root.innerHTML = "";
  const select = document.createElement("select");
  select.className = "model-selector";
  select.id = "header-model-select";
  select.setAttribute("aria-label", "Model and endpoint");

  const placeholder = document.createElement("option");
  placeholder.value = "";
  placeholder.textContent = "Choose a model…";
  select.appendChild(placeholder);

  // option.value is an index into this array rather than an encoded
  // "endpointId:modelId" string, since a model ID could itself contain
  // the separator character.
  const optionTargets = [];

  let endpoints = [];
  try {
    endpoints = await api.listEndpoints();
  } catch {
    endpoints = [];
  }

  for (const ep of endpoints) {
    let models = [];
    try {
      models = await api.listModels(ep.id);
    } catch {
      continue;
    }
    const chatModels = models.filter((m) => m.chat);
    if (chatModels.length === 0) continue;

    const group = document.createElement("optgroup");
    group.label = `${ep.name} (${ep.privacy_class})`;
    for (const m of chatModels) {
      const option = document.createElement("option");
      option.value = String(optionTargets.length);
      optionTargets.push({ endpointId: ep.id, modelId: m.id });

      const contextLabel = m.context_length ? `, ${Math.round(m.context_length / 1024)}k ctx` : "";
      option.textContent = `${m.id} (${m.state}${contextLabel})`;
      if (ep.id === currentEndpointId && m.id === currentModelId) {
        option.selected = true;
      }
      group.appendChild(option);
    }
    select.appendChild(group);
  }

  let savedValue = select.value;
  select.addEventListener("change", async () => {
    if (select.value === "") return;
    const target = optionTargets[Number(select.value)];
    select.disabled = true;
    try {
      await onChange(target.endpointId, target.modelId);
      savedValue = select.value;
    } catch {
      select.value = savedValue;
    }
    finally { select.disabled = false; }
  });

  root.appendChild(select);
}
