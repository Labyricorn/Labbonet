// Copyright (C) 2026 Elodine / GoblinCorps. AGPL-3.0-or-later.
// Sidebar adapted from GobboNet chat.html @ 314bdc9cae204ae38fdc1002ac6a9fb1437c109f.
// Modified by the Labbonet project, 2026-10-17: conversation API and safe DOM handlers.
// Labbonet additions/modifications: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Upstream credit covers presentation structure/design (thread list, folder, tag and drag classes);
// runtime/API/state/security code, including folder/tag/pin/order behavior, is Labbonet work.
// Element-by-element source and authorship boundary: docs/REUSE-LOG.md and root NOTICE.md.
import { api, dataApi } from "./api.js";
import { mountChatPanel, loadConversationIntoPanel } from "./chat.js";
import { showForm } from "./dialogs.js";

const COLLAPSED_KEY = "labbonet.collapsedFolders";

function readCollapsed() {
  try {
    const parsed = JSON.parse(localStorage.getItem(COLLAPSED_KEY) || "[]");
    return new Set(Array.isArray(parsed) ? parsed.filter(Number.isInteger) : []);
  } catch { return new Set(); }
}
function writeCollapsed(set) {
  try { localStorage.setItem(COLLAPSED_KEY, JSON.stringify([...set])); } catch { /* optional convenience */ }
}

function el(tag, cls, text) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (text !== undefined) node.textContent = text;
  return node;
}

export function mountChatView(panel) {
  const list = document.getElementById("thread-list");
  const newBtn = document.getElementById("new-conversation");
  const newFolderBtn = document.getElementById("new-folder");
  const search = document.getElementById("thread-search");
  const clear = document.getElementById("thread-search-clear");
  let selectedId = null;
  let conversations = [];
  let folders = [];
  let hits = null; // Map(conversation id -> hit) while a content search is active
  let searchSeq = 0;
  let searchTimer = null;
  let dragId = null;
  const collapsed = readCollapsed();
  mountChatPanel(panel);

  // "#tag" tokens filter by tag; the remaining text searches titles, tags and message text.
  function parseQuery() {
    const tags = [];
    const words = [];
    for (const token of search.value.trim().split(/\s+/).filter(Boolean)) {
      if (token.startsWith("#") && token.length > 1) tags.push(token.slice(1).toLowerCase());
      else words.push(token);
    }
    return { tags, text: words.join(" ") };
  }

  function visibleConversations() {
    const { tags, text } = parseQuery();
    return conversations.filter((c) => {
      if (tags.some((t) => !c.tags.some((x) => x.toLowerCase() === t))) return false;
      if (!text) return true;
      if (hits) return hits.has(c.id);
      return (c.title || "Untitled").toLowerCase().includes(text.toLowerCase());
    });
  }

  function threadItem(conv) {
    const li = el("li", `thread-item${conv.id === selectedId ? " active" : ""}${conv.pinned ? " thread-pinned" : ""}`);
    li.draggable = true;
    li.dataset.id = String(conv.id);
    const body = el("button", "thread-body");
    body.type = "button";
    body.title = conv.title || "Untitled";
    const nameRow = el("div", "thread-name-row");
    nameRow.append(el("span", "thread-name", conv.title || "Untitled"));
    if (conv.pinned) { const badge = el("span", "thread-pin-badge", "★"); badge.title = "Pinned"; nameRow.append(badge); }
    body.append(nameRow);
    const hit = hits?.get(conv.id);
    if (hit?.snippet) body.append(el("div", "thread-snippet", hit.snippet));
    if (conv.tags.length) {
      const row = el("div", "thread-tags-row");
      for (const tag of conv.tags) {
        const pill = el("span", "tag-pill", tag);
        pill.title = `Filter by #${tag}`;
        pill.addEventListener("click", (event) => { event.stopPropagation(); search.value = `#${tag}`; onSearchInput(); });
        row.append(pill);
      }
      body.append(row);
    }
    body.addEventListener("click", () => selectConversation(conv.id));

    const controls = el("div", "thread-ctrl");
    const ctrl = (text, label, handler, cls = "") => {
      const b = el("button", `thread-ctrl-btn ${cls}`.trim(), text);
      b.type = "button";
      b.title = label;
      b.setAttribute("aria-label", `${label}: ${conv.title || "Untitled"}`);
      b.addEventListener("click", (event) => { event.stopPropagation(); handler(); });
      return b;
    };
    controls.append(
      ctrl(conv.pinned ? "★" : "☆", conv.pinned ? "Unpin" : "Pin", () => organize(conv.id, { pinned: !conv.pinned })),
      ctrl("#", "Edit tags", () => editTags(conv)),
      ctrl("▤", "Move to folder", () => moveToFolder(conv)),
      ctrl("⇩", "Export thread", () => exportThread(conv), "thread-export-btn"),
      ctrl("×", "Delete", () => removeConversation(conv), "thread-del-btn"),
    );
    li.append(body, controls);
    attachDrag(li, conv);
    return li;
  }

  function attachDrag(li, conv) {
    li.addEventListener("dragstart", (event) => {
      dragId = conv.id;
      event.dataTransfer.effectAllowed = "move";
      try { event.dataTransfer.setData("text/plain", String(conv.id)); } catch { /* some engines refuse */ }
      requestAnimationFrame(() => li.classList.add("dragging"));
    });
    li.addEventListener("dragend", () => { dragId = null; clearDropMarks(); li.classList.remove("dragging"); });
    li.addEventListener("dragover", (event) => {
      if (dragId === null || dragId === conv.id) return;
      event.preventDefault();
      const rect = li.getBoundingClientRect();
      const before = event.clientY < rect.top + rect.height / 2;
      li.classList.toggle("drag-over-before", before);
      li.classList.toggle("drag-over-after", !before);
    });
    li.addEventListener("dragleave", () => li.classList.remove("drag-over-before", "drag-over-after"));
    li.addEventListener("drop", (event) => {
      if (dragId === null || dragId === conv.id) return;
      event.preventDefault();
      const rect = li.getBoundingClientRect();
      const before = event.clientY < rect.top + rect.height / 2;
      const moved = dragId;
      dragId = null;
      clearDropMarks();
      dropOnThread(moved, conv, before);
    });
  }

  function clearDropMarks() {
    for (const node of list.querySelectorAll(".drag-over-before,.drag-over-after,.drag-folder-over")) {
      node.classList.remove("drag-over-before", "drag-over-after", "drag-folder-over");
    }
  }

  function dropZone(node, folderId) {
    node.addEventListener("dragover", (event) => {
      if (dragId === null) return;
      event.preventDefault();
      node.classList.add("drag-folder-over");
    });
    node.addEventListener("dragleave", () => node.classList.remove("drag-folder-over"));
    node.addEventListener("drop", (event) => {
      if (dragId === null) return;
      event.preventDefault();
      const moved = dragId;
      dragId = null;
      clearDropMarks();
      organize(moved, folderId === null ? { clear_folder: true } : { folder_id: folderId });
    });
  }

  async function dropOnThread(movedId, target, before) {
    const moved = conversations.find((c) => c.id === movedId);
    if (!moved || moved.id === target.id) return;
    if (moved.pinned !== target.pinned) { alert("Pinned threads stay above unpinned ones. Pin or unpin the thread to move it across."); return; }
    const order = conversations.map((c) => c.id).filter((id) => id !== movedId);
    const at = order.indexOf(target.id) + (before ? 0 : 1);
    order.splice(at, 0, movedId);
    try {
      if ((moved.folder_id ?? null) !== (target.folder_id ?? null)) {
        await api.organizeConversation(movedId, target.folder_id ? { folder_id: target.folder_id } : { clear_folder: true });
      }
      await api.reorderConversations(order);
    } catch (err) { alert(err.status === 409 ? "The thread list changed elsewhere; it was reloaded." : err.message); }
    await refreshList();
  }

  function folderHeader(folder, count) {
    const li = el("li", "folder-hdr");
    li.dataset.folder = String(folder.id);
    const isCollapsed = collapsed.has(folder.id);
    const toggle = el("button", "folder-toggle", `${isCollapsed ? "▸" : "▾"} ${folder.name}`);
    toggle.type = "button";
    toggle.setAttribute("aria-expanded", String(!isCollapsed));
    toggle.addEventListener("click", () => {
      if (collapsed.has(folder.id)) collapsed.delete(folder.id); else collapsed.add(folder.id);
      writeCollapsed(collapsed);
      renderList();
    });
    const badge = el("span", "folder-count", String(count));
    const rename = el("button", "thread-ctrl-btn", "✎");
    rename.type = "button"; rename.title = "Rename folder"; rename.setAttribute("aria-label", `Rename folder ${folder.name}`);
    rename.addEventListener("click", async () => {
      const name = await showForm({ title: "RENAME FOLDER", label: "Folder name", value: folder.name, validate: validateFolderName });
      if (name === null) return;
      try { await api.renameFolder(folder.id, name); } catch (err) { alert(err.message); }
      await refreshList();
    });
    const remove = el("button", "thread-ctrl-btn thread-del-btn", "×");
    remove.type = "button"; remove.title = "Delete folder"; remove.setAttribute("aria-label", `Delete folder ${folder.name}`);
    remove.addEventListener("click", async () => {
      if (!confirm(`Delete folder "${folder.name}"? Its threads are kept and move to Unfiled.`)) return;
      try { await api.deleteFolder(folder.id); } catch (err) { alert(err.message); }
      await refreshList();
    });
    li.append(toggle, badge, rename, remove);
    dropZone(li, folder.id);
    return li;
  }

  function validateFolderName(value) {
    const name = value.trim();
    if (!name) return "Enter a name.";
    if ([...name].length > 60) return "Use 60 characters or fewer.";
    return "";
  }

  function renderList() {
    list.replaceChildren();
    clear.hidden = !search.value;
    const shown = visibleConversations();
    const searching = Boolean(search.value.trim());
    const append = (items) => { for (const conv of items) list.append(threadItem(conv)); };
    for (const folder of folders) {
      const inFolder = shown.filter((c) => c.folder_id === folder.id);
      if (searching && inFolder.length === 0) continue;
      list.append(folderHeader(folder, inFolder.length));
      if (!collapsed.has(folder.id) || searching) append(inFolder);
    }
    const unfiled = shown.filter((c) => c.folder_id === null || !folders.some((f) => f.id === c.folder_id));
    if (folders.length) {
      const label = el("li", "unfiled-label", "UNFILED");
      dropZone(label, null);
      list.append(label);
    }
    append(unfiled);
    if (searching && shown.length === 0) list.append(el("li", "thread-empty", "No matching threads"));
  }

  async function refreshList() {
    try {
      [conversations, folders] = await Promise.all([api.listConversations(), api.listFolders()]);
    } catch { return; }
    renderList();
  }

  async function organize(id, body) {
    try { await api.organizeConversation(id, body); } catch (err) { alert(err.message); }
    await refreshList();
  }

  async function editTags(conv) {
    const value = await showForm({
      title: "THREAD TAGS", label: "Tags (comma separated)", value: conv.tags.join(", "),
      hint: "Up to 16 tags of 32 characters. Click a tag in the list to filter by it.",
      validate: (v) => {
        const tags = v.split(",").map((t) => t.trim()).filter(Boolean);
        if (tags.length > 16) return "Use at most 16 tags.";
        if (tags.some((t) => [...t].length > 32)) return "Each tag must be 32 characters or fewer.";
        return "";
      },
    });
    if (value === null) return;
    await organize(conv.id, { tags: value.split(",").map((t) => t.trim()).filter(Boolean) });
  }

  async function moveToFolder(conv) {
    if (!folders.length) { alert("Create a folder first with + FOLDER."); return; }
    const choice = await showForm({
      title: "MOVE TO FOLDER", label: "Folder", confirmText: "MOVE",
      options: [["", "Unfiled"], ...folders.map((f) => [String(f.id), f.name])], value: conv.folder_id ? String(conv.folder_id) : "",
    });
    if (choice === null) return;
    await organize(conv.id, choice ? { folder_id: Number(choice) } : { clear_folder: true });
  }

  async function exportThread(conv) {
    try { await dataApi.download(dataApi.exportDataPath("conversation", conv.id)); } catch (err) { alert(err.message); }
  }

  async function removeConversation(conv) {
    if (!confirm("Delete this conversation and all its messages?")) return;
    try {
      await api.deleteConversation(conv.id);
      if (selectedId === conv.id) { selectedId = null; mountChatPanel(panel); }
      await refreshList();
    } catch (err) { alert(err.message); }
  }

  async function selectConversation(id) {
    selectedId = id;
    renderList();
    // Show the chat view without firing HOME's dashboard navigation handler.
    for (const el of document.querySelectorAll(".view")) el.hidden = el.id !== "view-chat";
    for (const btn of document.querySelectorAll("[data-view]")) btn.classList.toggle("active", btn.dataset.view === "chat");
    if (matchMedia("(max-width: 700px)").matches) {
      document.getElementById("sidebar").classList.add("collapsed");
      document.getElementById("sidebar-overlay").classList.remove("open");
    }
    try {
      await loadConversationIntoPanel(panel, id, {
        onRenamed: refreshList,
        onForked: async (forkedId) => { await refreshList(); await selectConversation(forkedId); },
      });
    } catch (err) { alert(`Could not open conversation: ${err.message}`); }
  }

  newBtn.addEventListener("click", async () => {
    try {
      const conv = await api.createConversation({ title: "New thread" });
      await refreshList();
      await selectConversation(conv.id);
    } catch (err) { alert(err.message); }
  });
  for (const home of document.querySelectorAll('[data-view="chat"]')) home.addEventListener("click", () => { selectedId = null; mountChatPanel(panel); renderList(); });
  document.addEventListener("open-thread", event => selectConversation(event.detail.id));
  document.addEventListener("new-thread", async event => {
    try {
      const conv = await api.createConversation({ title: "New thread" });
      if (event.detail?.character_id) {
        const cast = await api.getCast(conv.id);
        await api.saveCast(conv.id, { ...cast, character_id: event.detail.character_id, greeting: 0 });
      }
      await refreshList(); await selectConversation(conv.id);
    } catch (err) { alert(err.message); }
  });
  newFolderBtn.addEventListener("click", async () => {
    const name = await showForm({ title: "NEW FOLDER", label: "Folder name", validate: validateFolderName, confirmText: "CREATE" });
    if (name === null) return;
    try { await api.createFolder(name.trim()); } catch (err) { alert(err.message); }
    await refreshList();
  });
  // Import or reset elsewhere may add or remove threads.
  document.addEventListener("data-changed", async () => {
    await refreshList();
    if (selectedId !== null && !conversations.some(c => c.id === selectedId)) { selectedId = null; mountChatPanel(panel); renderList(); }
  });

  // Titles filter instantly; text of two or more characters also searches tags and message
  // content (all branches) on the server, after a short pause.
  function onSearchInput() {
    clearTimeout(searchTimer);
    const seq = ++searchSeq;
    const { text } = parseQuery();
    clear.hidden = !search.value;
    hits = null;
    renderList();
    if ([...text].length < 2) return;
    searchTimer = setTimeout(async () => {
      try {
        const result = await api.searchConversations(text);
        if (seq !== searchSeq) return;
        hits = new Map(result.map((h) => [h.conversation_id, h]));
      } catch {
        if (seq !== searchSeq) return;
        hits = null;
      }
      renderList();
    }, 250);
  }
  search.addEventListener("input", onSearchInput);
  clear.addEventListener("click", () => { search.value = ""; onSearchInput(); search.focus(); });
  refreshList();
}
