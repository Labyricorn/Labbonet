// Manual S14 check: feeds the real renderMarkdown pipeline a handful
// of inputs modeled on the exact SECURITY-MODEL.md S14 payloads
// (a <script> tag, an <img onerror>, a "javascript:" link) plus one
// legitimate Markdown input, and confirms none of the hostile ones
// ran and the legitimate one still rendered correctly.
import { renderMarkdown } from "../js/render.js";

const cases = [
  {
    name: "safe markdown renders (bold, code, link)",
    input: "**bold** and `code` and a [link](https://example.com)",
    check: (node) =>
      !!node.querySelector("strong") && !!node.querySelector("code") && !!node.querySelector("a"),
  },
  {
    name: "link gets rel=noopener and target=_blank",
    input: "[link](https://example.com)",
    check: (node) => {
      const a = node.querySelector("a");
      return !!a && a.getAttribute("rel") === "noopener noreferrer" && a.getAttribute("target") === "_blank";
    },
  },
  {
    name: "<script> tag is neutralized (S14)",
    input: "<script>window.xssFired = true;</script>",
    check: (node) => node.querySelector("script") === null,
  },
  {
    name: "<img onerror=...> is neutralized (S14)",
    input: '<img src="x" onerror="window.xssFired = true">',
    check: (node) => {
      const img = node.querySelector("img");
      return img === null || !img.hasAttribute("onerror");
    },
  },
  {
    name: '"javascript:" link is neutralized (S14)',
    input: "[click me](javascript:window.xssFired=true)",
    check: (node) => {
      const a = node.querySelector("a");
      return a === null || !/^javascript:/i.test(a.getAttribute("href") || "");
    },
  },
];

const results = document.getElementById("results");

document.addEventListener("securitypolicyviolation", (e) => {
  const div = document.createElement("div");
  div.className = "csp-violation";
  div.textContent = `CSP violation: ${e.violatedDirective} blocked ${e.blockedURI}`;
  results.prepend(div);
});

window.xssFired = false;

for (const testCase of cases) {
  window.xssFired = false;
  const node = renderMarkdown(testCase.input);
  const checkPassed = testCase.check(node);
  const noScriptRan = window.xssFired === false;
  const pass = checkPassed && noScriptRan;

  const row = document.createElement("div");
  row.className = `render-test-case ${pass ? "pass" : "fail"}`;

  const title = document.createElement("strong");
  title.textContent = `${pass ? "PASS" : "FAIL"} — ${testCase.name}`;

  const preview = document.createElement("div");
  preview.className = "render-preview";
  preview.appendChild(node);

  row.append(title, preview);
  results.appendChild(row);
}
