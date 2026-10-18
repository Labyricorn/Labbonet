// Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Chat tools (parity milestone 4): code copy/save, rendered/source toggle, text-file
// attachments, per-viewer reply limits and scroll-follow. Labbonet-original runtime code;
// it reuses pinned msg-action-btn presentation classes. GobboNet's code/file actions
// (js/18-utils.js, js/13-dashboard.js) were a behavior reference only; no upstream code is copied.
// All content here is untrusted text: it is only ever assigned through textContent or
// Blob, never parsed as HTML.

const PREFS_KEY = "labbonet.chat.prefs";
const DEFAULT_PREFS = { replyCap: 0, reasoningLimit: 0, follow: true };

// Per-viewer conveniences live in localStorage; the page works when it is unavailable.
export function loadPrefs() {
  try {
    const raw = JSON.parse(localStorage.getItem(PREFS_KEY) || "{}");
    return {
      replyCap: Number.isInteger(raw.replyCap) && raw.replyCap >= 0 ? raw.replyCap : 0,
      reasoningLimit: Number.isInteger(raw.reasoningLimit) && raw.reasoningLimit >= 0 ? raw.reasoningLimit : 0,
      follow: raw.follow !== false,
    };
  } catch {
    return { ...DEFAULT_PREFS };
  }
}

export function savePrefs(prefs) {
  try { localStorage.setItem(PREFS_KEY, JSON.stringify(prefs)); } catch { /* storage unavailable */ }
}

// --- Code blocks -------------------------------------------------------------------

const EXTENSIONS = {
  js: "js", javascript: "js", mjs: "mjs", ts: "ts", typescript: "ts", json: "json", html: "html", css: "css",
  py: "py", python: "py", go: "go", rust: "rs", rs: "rs", c: "c", cpp: "cpp", java: "java", cs: "cs", csharp: "cs",
  sh: "sh", bash: "sh", shell: "sh", zsh: "sh", powershell: "ps1", ps1: "ps1", sql: "sql", yaml: "yaml", yml: "yml",
  toml: "toml", xml: "xml", md: "md", markdown: "md", txt: "txt", text: "txt", csv: "csv", ini: "ini", lua: "lua",
  rb: "rb", ruby: "rb", php: "php", swift: "swift", kotlin: "kt",
};

// codeFileName picks a download name from the fence language. Only known extensions
// are used; the name never contains model-supplied text.
export function codeFileName(lang, index) {
  const ext = EXTENSIONS[(lang || "").toLowerCase()] || "txt";
  return `labbonet-code-${index + 1}.${ext}`;
}

export function saveTextFile(name, text) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain;charset=utf-8" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = name;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 10000);
}

function toolButton(label, title, handler) {
  const button = document.createElement("button");
  button.type = "button";
  button.className = "msg-action-btn code-btn";
  button.textContent = label;
  button.title = title;
  button.addEventListener("click", handler);
  return button;
}

async function copyToClipboard(text, button) {
  const old = button.textContent;
  try {
    await navigator.clipboard.writeText(text);
    button.textContent = "COPIED";
  } catch {
    button.textContent = "NO CLIPBOARD";
  }
  setTimeout(() => { button.textContent = old; }, 1200);
}

// decorateCode adds a Copy and Save bar above every fenced code block in a rendered message.
export function decorateCode(container) {
  container.querySelectorAll("pre").forEach((pre, index) => {
    const code = pre.querySelector("code");
    if (!code || pre.parentElement?.classList.contains("code-block")) return;
    const lang = [...code.classList].map((c) => /^language-(.+)$/.exec(c)?.[1]).find(Boolean) || "";
    const wrap = document.createElement("div");
    wrap.className = "code-block";
    const bar = document.createElement("div");
    bar.className = "code-toolbar";
    const label = document.createElement("span");
    label.className = "code-lang";
    label.textContent = lang || "text";
    const copy = toolButton("Copy", "Copy this code", () => copyToClipboard(code.textContent, copy));
    const save = toolButton("Save", "Save this code as a file", () => saveTextFile(codeFileName(lang, index), code.textContent));
    bar.append(label, copy, save);
    pre.replaceWith(wrap);
    wrap.append(bar, pre);
  });
}

// --- Rendered / source toggle ------------------------------------------------------

// sourceToggle swaps a message body between rendered Markdown and its literal text.
export function sourceToggle(contentEl, text, render) {
  let showingSource = false;
  const button = toolButton("Source", "Show the message as plain text", () => {
    showingSource = !showingSource;
    if (showingSource) {
      const pre = document.createElement("pre");
      pre.className = "msg-source";
      pre.textContent = text;
      contentEl.replaceChildren(pre);
    } else {
      contentEl.replaceChildren(render());
    }
    button.textContent = showingSource ? "Rendered" : "Source";
    button.title = showingSource ? "Show the formatted message" : "Show the message as plain text";
  });
  button.classList.remove("code-btn");
  return button;
}

// --- Attachments -------------------------------------------------------------------

// Keep these in step with generate.MaxAttachments and friends; the server re-checks all of them.
export const ATTACH_LIMITS = { files: 5, fileBytes: 256 * 1024, totalBytes: 512 * 1024 };
const TEXT_EXTENSIONS = /\.(txt|md|markdown|json|csv|tsv|log|xml|yaml|yml|toml|ini|cfg|conf|html|htm|css|js|mjs|ts|py|go|rs|c|h|cpp|hpp|java|cs|sh|ps1|sql|lua|rb|php|swift|kt|tex|rst)$/i;

// readAttachment reads one file as text or explains why it was refused.
export async function readAttachment(file) {
  if (file.size > ATTACH_LIMITS.fileBytes) {
    throw new Error(`${file.name} is larger than ${ATTACH_LIMITS.fileBytes / 1024} KiB.`);
  }
  if (!(file.type.startsWith("text/") || TEXT_EXTENSIONS.test(file.name) || file.type === "application/json")) {
    throw new Error(`${file.name} does not look like a text file.`);
  }
  const buffer = await file.arrayBuffer();
  let text;
  try {
    text = new TextDecoder("utf-8", { fatal: true }).decode(buffer);
  } catch {
    throw new Error(`${file.name} is not valid UTF-8 text.`);
  }
  if (text.includes("\u0000")) throw new Error(`${file.name} is not a text file.`);
  return { name: file.name, content: text };
}

// addAttachments appends readable files to list, returning the problems found. The list
// is bounded by file count and total size.
export async function addAttachments(list, files) {
  const problems = [];
  for (const file of files) {
    if (list.length >= ATTACH_LIMITS.files) {
      problems.push(`At most ${ATTACH_LIMITS.files} files can be attached.`);
      break;
    }
    try {
      const item = await readAttachment(file);
      const total = list.reduce((sum, a) => sum + new Blob([a.content]).size, 0) + new Blob([item.content]).size;
      if (total > ATTACH_LIMITS.totalBytes) throw new Error(`Attachments may total at most ${ATTACH_LIMITS.totalBytes / 1024} KiB.`);
      list.push(item);
    } catch (err) {
      problems.push(err.message);
    }
  }
  return problems;
}

// --- Scroll follow -----------------------------------------------------------------

// createFollow keeps a streaming thread pinned to the bottom only while the reader is
// there. Scrolling up releases it; the jump button (or scrolling back down) re-engages.
export function createFollow(thread, jumpButton, enabled) {
  let following = true;
  const nearBottom = () => thread.scrollHeight - thread.scrollTop - thread.clientHeight < 48;
  const sync = () => { jumpButton.hidden = nearBottom(); };
  thread.addEventListener("scroll", () => {
    following = nearBottom();
    sync();
  }, { passive: true });
  jumpButton.addEventListener("click", () => {
    following = true;
    thread.scrollTop = thread.scrollHeight;
    sync();
  });
  return {
    // Call after the thread content changed. force scrolls regardless (a new turn).
    update(force = false) {
      if (force || (enabled() && following)) {
        if (force) following = true;
        thread.scrollTop = thread.scrollHeight;
      }
      sync();
    },
    refresh: sync,
  };
}
