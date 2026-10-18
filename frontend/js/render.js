// Markdown rendering with raw HTML disabled, then sanitized
// (SECURITY-MODEL.md section 6: "Model output ... rendered as Markdown
// with raw HTML disabled, then sanitized. Links get
// rel="noopener noreferrer" and open in a new tab only on user click.").
// window.markdownit and window.DOMPurify are set by the vendored
// classic scripts in frontend/vendor/ (see vendor/VENDOR.md); this
// module never fetches, builds or evaluates anything itself.

const md = window.markdownit({ html: false, linkify: true, breaks: true });

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
