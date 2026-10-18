// Markdown rendering with raw HTML disabled, then sanitized
// (SECURITY-MODEL.md section 6: "Model output ... rendered as Markdown
// with raw HTML disabled, then sanitized. Links get
// rel="noopener noreferrer" and open in a new tab only on user click.").
// window.markdownit and window.DOMPurify are set by the vendored
// classic scripts in frontend/vendor/ (see vendor/VENDOR.md); this
// module never fetches, builds or evaluates anything itself.

// Original integration: Copyright (C) 2026 Labbonet contributors. AGPL-3.0-or-later.
// Code/math behavior reference: GobboNet js/18-utils.js at the pinned commit;
// Copyright (C) 2026 Elodine / GoblinCorps. No upstream runtime copied.
// KaTeX/highlight.js have independent licenses; see vendor/VENDOR.md and REUSE-LOG.md.
const md = window.markdownit({ html: false, linkify: true, breaks: true,
  highlight(code, language) {
    if (code.length > 32768 || !window.hljs?.getLanguage(language)) return "";
    try { return window.hljs.highlight(code, { language, ignoreIllegals: true }).value; }
    catch { return ""; }
  },
});

function math(source, display) {
  if (source.length <= 8192 && window.katex) {
    try {
      return window.katex.renderToString(source, { output: "mathml", displayMode: display,
        trust: false, strict: "error", throwOnError: true, maxExpand: 200, maxSize: 10 });
    } catch { /* Invalid, unsupported or excessive TeX remains readable source. */ }
  }
  return `<code class="math-source">${md.utils.escapeHtml(source)}</code>`;
}

// Rules operate on Markdown tokens, so code, escaped delimiters and link attributes
// never become math. A single-dollar opener must not be followed by whitespace;
// the closer must not be followed by a digit (ordinary currency stays ordinary text).
md.inline.ruler.before("escape", "math", (state, silent) => {
  const start = state.pos;
  const rest = state.src.slice(start);
  const open = rest.startsWith("\\(") ? "\\(" : rest.startsWith("$$") ? "$$" : rest.startsWith("$") ? "$" : "";
  if (!open || (open === "$" && /\s/.test(rest[1] || " "))) return false;
  const close = open === "\\(" ? "\\)" : open;
  let end = start + open.length;
  for (; end < state.posMax; end++) {
    if (state.src[end] === "\\" && !state.src.startsWith(close, end)) { end++; continue; }
    if (state.src.startsWith(close, end)) break;
  }
  if (end >= state.posMax || end === start + open.length) return false;
  if (open === "$" && (/\s/.test(state.src[end - 1]) || /\d/.test(state.src[end + 1] || ""))) return false;
  if (!silent) {
    const token = state.push("math", "math", 0);
    token.content = state.src.slice(start + open.length, end);
    token.block = open === "$$";
  }
  state.pos = end + close.length; return true;
});
md.renderer.rules.math = (tokens, idx) => math(tokens[idx].content, tokens[idx].block);
md.block.ruler.before("fence", "math_block", (state, startLine, endLine, silent) => {
  const first = state.src.slice(state.bMarks[startLine] + state.tShift[startLine], state.eMarks[startLine]).trim();
  if (first !== "$$" && first !== "\\[") return false;
  const close = first === "$$" ? "$$" : "\\]";
  let next = startLine + 1;
  while (next < endLine && state.src.slice(state.bMarks[next], state.eMarks[next]).trim() !== close) next++;
  if (next === endLine) return false;
  if (!silent) {
    const token = state.push("math", "math", 0); token.block = true;
    token.content = state.getLines(startLine + 1, next, state.blkIndent, false);
    token.map = [startLine, next + 1];
  }
  state.line = next + 1; return true;
});
const fence = md.renderer.rules.fence;
md.renderer.rules.fence = (tokens, idx, options, env, self) =>
  /^(math|latex|tex|equation|displaymath)$/i.test(tokens[idx].info.trim())
    ? math(tokens[idx].content, true) : fence(tokens, idx, options, env, self);

const defaultLinkOpen =
  md.renderer.rules.link_open ||
  function (tokens, idx, options, _env, self) {
    return self.renderToken(tokens, idx, options);
  };
md.renderer.rules.link_open = function (tokens, idx, options, env, self) {
  const token = tokens[idx];
  token.attrSet("rel", "noopener noreferrer");
  token.attrSet("target", "_blank");
  return defaultLinkOpen(tokens, idx, options, env, self);
};

// Only http(s), mailto, and scheme-less (relative) URIs survive
// sanitization; this is what stops a "javascript:" link markdown can
// otherwise produce. Pattern shape follows DOMPurify's own default,
// narrowed to the schemes Labbonet actually needs.
//
// ADD_ATTR: DOMPurify's default allowlist does not include "target"
// (confirmed against the vendored build, frontend/tests/render.html),
// so without this the target="_blank" set above would be silently
// stripped during sanitization while "rel" survived untouched.
const purifyConfig = {
  ALLOWED_URI_REGEXP: /^(?:(?:https?|mailto):|[^a-z]|[a-z+.-]+(?:[^a-z+.:-]|$))/i,
  ADD_ATTR: ["target"],
  FORBID_ATTR: ["style"],
};

// renderMarkdown turns model output (or any other untrusted text) into
// a safe, detached DOM node. Callers must use this (never innerHTML
// with a raw string) for anything that came from a model or imported
// content.
export function renderMarkdown(text) {
  const rawHtml = md.render(text ?? "");
  const clean = window.DOMPurify.sanitize(rawHtml, purifyConfig);
  const container = document.createElement("div");
  container.innerHTML = clean; // safe: `clean` is DOMPurify's own output
  return container;
}
