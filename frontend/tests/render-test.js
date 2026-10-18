// Manual S14 check: feeds the real renderMarkdown pipeline a handful
// of inputs modeled on the exact SECURITY-MODEL.md S14 payloads
// (a <script> tag, an <img onerror>, a "javascript:" link) plus one
// legitimate Markdown input, and confirms none of the hostile ones
// ran and the legitimate one still rendered correctly.
import { renderMarkdown } from "../js/render.js";
import { codeFileName } from "../js/tools.js";

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

cases.push(
  { name: "inline and block TeX become native MathML", input: "Inline $x^2$ and \\(y_1\\).\n\n$$\n\\frac{a}{b}\n$$", check: n => n.querySelectorAll("math").length === 3 && !!n.querySelector('math[display="block"]') },
  { name: "math fences render; code and escaped dollars remain literal", input: "`$x$` and \\$5.\n\n```math\n\\sqrt{x}\n```", check: n => n.querySelectorAll("math").length === 1 && n.querySelector("code").textContent === "$x$" },
  { name: "currency and unclosed math stay readable", input: "Cost $5 and $10. Unclosed $x", check: n => !n.querySelector("math") && n.textContent.includes("$5") },
  { name: "trusted TeX commands cannot make links, images or styles", input: "$\\href{javascript:alert(1)}{x}$ $\\includegraphics{https://example.com/x}$ $\\htmlStyle{background:url(x)}{x}$", check: n => !n.querySelector("a,img,style,script,[style]") },
  { name: "expansion bomb and malformed TeX fall back to source", input: "$\\def\\a{\\a}\\a$ and $\\frac{$", check: n => n.querySelectorAll(".math-source").length === 2 && !n.querySelector("[style]") },
  { name: "highlighted code retains its exact text", input: '```javascript\nconst x = "<script>";\n```', check: n => !!n.querySelector(".hljs-keyword") && n.querySelector("code").textContent === 'const x = "<script>";\n' && !n.querySelector("script") },
  { name: "unknown code language remains escaped text", input: '```unknown\n<img onerror=alert(1)>\n```', check: n => !n.querySelector("img") && n.querySelector("code").textContent.includes("<img") },
  { name: "generated-file names cannot carry paths or reserved devices", input: "files", check: () => codeFileName("file:../../report.json", 0) === "report.json" && codeFileName("file:C:\\tmp\\report.csv", 0) === "report.csv" && codeFileName("file:CON.txt", 0) === "labbonet-code-1.txt" },
);

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
