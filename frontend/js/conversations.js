// Conversation list sidebar (ARCHITECTURE.md section 3): list,
// create, select, and the delegated chat panel for the selected one.
import { api } from "./api.js";
import { mountChatPanel, loadConversationIntoPanel } from "./chat.js";

export function mountChatView(root) {
  root.innerHTML = "";
  root.classList.add("chat-view");

  const sidebar = document.createElement("aside");
  sidebar.className = "conversation-sidebar";
  const newBtn = document.createElement("button");
  newBtn.type = "button";
  newBtn.className = "new-conversation";
  newBtn.textContent = "New conversation";
  const list = document.createElement("ul");
  list.className = "conversation-list";
  sidebar.append(newBtn, list);

  const panel = document.createElement("div");
  panel.className = "chat-panel";

  root.append(sidebar, panel);
  mountChatPanel(panel);

  let selectedId = null;

  async function refreshList() {
    let conversations = [];
    try {
      conversations = await api.listConversations();
    } catch {
      return;
    }
    list.innerHTML = "";
    for (const conv of conversations) {
      const li = document.createElement("li");
      li.textContent = conv.title || "Untitled";
      li.className = conv.id === selectedId ? "selected" : "";
      li.addEventListener("click", () => selectConversation(conv.id));
      list.appendChild(li);
    }
  }

  async function selectConversation(id) {
    selectedId = id;
    await refreshList();
    await loadConversationIntoPanel(panel, id, {
      onRenamed: refreshList,
      onDeleted: async () => {
        selectedId = null;
        mountChatPanel(panel);
        await refreshList();
      },
    });
  }

  newBtn.addEventListener("click", async () => {
    const conv = await api.createConversation({ title: "New conversation" });
    await selectConversation(conv.id);
  });

  refreshList();
}
