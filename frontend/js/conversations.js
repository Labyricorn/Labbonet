// Copyright (C) 2026 Elodine / GoblinCorps. AGPL-3.0-or-later.
// Sidebar adapted from GobboNet chat.html @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f.
// Modified by the Labbonet project, 2026-10-17: conversation API and safe DOM handlers.
// Labbonet additions/modifications: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Upstream credit covers presentation structure/design; runtime/API/state/security code is Labbonet work.
// Element-by-element source and authorship boundary: docs/REUSE-LOG.md and root NOTICE.md.
import { api } from "./api.js";
import { mountChatPanel, loadConversationIntoPanel } from "./chat.js";

export function mountChatView(panel) {
  const list = document.getElementById("thread-list");
  const newBtn = document.getElementById("new-conversation");
  const search = document.getElementById("thread-search");
  const clear = document.getElementById("thread-search-clear");
  let selectedId = null;
  let conversations = [];
  mountChatPanel(panel);

  function renderList() {
    list.replaceChildren();
    const query = search.value.trim().toLowerCase();
    clear.hidden = !search.value;
    for (const conv of conversations.filter(c => (c.title || "Untitled").toLowerCase().includes(query))) {
      const li = document.createElement("li");
      li.className = `thread-item${conv.id === selectedId ? " active" : ""}`;
      const body = document.createElement("button");
      body.type = "button";
      body.className = "thread-body";
      body.title = conv.title || "Untitled";
      const name = document.createElement("span");
      name.className = "thread-name";
      name.textContent = conv.title || "Untitled";
      body.append(name);
      body.addEventListener("click", () => selectConversation(conv.id));
      const controls = document.createElement("div");
      controls.className = "thread-ctrl";
      const remove = document.createElement("button");
      remove.type = "button";
      remove.className = "thread-ctrl-btn thread-del-btn";
      remove.textContent = "×";
      remove.setAttribute("aria-label", `Delete ${conv.title || "Untitled"}`);
      remove.addEventListener("click", async () => {
        if (!confirm("Delete this conversation and all its messages?")) return;
        try {
          await api.deleteConversation(conv.id);
          if (selectedId === conv.id) { selectedId = null; mountChatPanel(panel); }
          await refreshList();
        } catch (err) { alert(err.message); }
      });
      controls.append(remove);
      li.append(body, controls);
      list.append(li);
    }
  }

  async function refreshList() {
    try { conversations = await api.listConversations(); } catch { return; }
    renderList();
  }

  async function selectConversation(id) {
    selectedId = id;
    renderList();
    document.getElementById("home-sidebar").click();
    if (matchMedia("(max-width: 700px)").matches) {
      document.getElementById("sidebar").classList.add("collapsed");
      document.getElementById("sidebar-overlay").classList.remove("open");
    }
    try { await loadConversationIntoPanel(panel, id, { onRenamed: refreshList }); }
    catch (err) { alert(`Could not open conversation: ${err.message}`); }
  }

  newBtn.addEventListener("click", async () => {
    try {
      const conv = await api.createConversation({ title: "New thread" });
      await refreshList();
      await selectConversation(conv.id);
    } catch (err) { alert(err.message); }
  });
  search.addEventListener("input", renderList);
  clear.addEventListener("click", () => { search.value = ""; renderList(); search.focus(); });
  refreshList();
}
