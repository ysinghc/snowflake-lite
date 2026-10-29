// Assembles site/content/**/*.html fragments into full pages under site/public.
// A fragment's first line is a JSON comment: <!--{"title":"…","desc":"…"}-->
// Fragments contain only the page body; layout, nav and the docs sidebar live here.
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const root = path.dirname(fileURLToPath(import.meta.url));
const src = path.join(root, "content");
const out = path.join(root, "public");

const DOCS = [
  ["Start", [["getting-started", "Getting started"], ["concepts", "How it works"]]],
  ["Library", [["api", "API reference"], ["faq", "FAQ and limits"]]],
  ["Tools", [["cli", "Command line"], ["http-server", "Simple HTTP server"]]],
  ["Platform", [["formats", "ID formats"], ["platform-api", "Platform API"], ["security-model", "Security model"], ["deployment", "Deployment"]]],
  ["Internals", [["architecture", "Architecture"], ["code-map", "Code map"]]],
];
const flat = DOCS.flatMap(([, items]) => items);

const esc = (s) => s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/"/g, "&quot;");
const slug = (s) => s.toLowerCase().replace(/<[^>]+>/g, "").replace(/&[a-z]+;/g, "").replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "");

function parse(file) {
  let html = fs.readFileSync(file, "utf8");
  const m = html.match(/^<!--(\{.*?\})-->\s*/s);
  const meta = m ? JSON.parse(m[1]) : {};
  if (m) html = html.slice(m[0].length);
  return { meta, html };
}

// Adds ids to h2/h3 and returns the h2 list for the "On this page" rail.
function anchors(html) {
  const toc = [];
  html = html.replace(/<h([23])>(.*?)<\/h\1>/g, (_, lvl, text) => {
    const id = slug(text);
    if (lvl === "2") toc.push([id, text.replace(/<[^>]+>/g, "")]);
    return `<h${lvl} id="${id}"><a class="anchor" href="#${id}" aria-label="Link to section">#</a>${text}</h${lvl}>`;
  });
  return { html, toc };
}

const header = (section) => `<header class="top"><div class="bar">
<a class="brand" href="/"><span class="flake" aria-hidden="true">❄</span>snowflake-lite</a>
<nav aria-label="Primary">
<a href="/docs/getting-started/"${section === "docs" ? ' aria-current="page"' : ""}>Docs</a>
<a href="/download/"${section === "download" ? ' aria-current="page"' : ""}>Download</a>
<a href="/community/"${section === "community" ? ' aria-current="page"' : ""}>Community</a>
<a href="/security/"${section === "security" ? ' aria-current="page"' : ""}>Security</a>
<a class="ext" href="https://github.com/ysinghc/snowflake-lite">GitHub</a>
</nav><span class="ver" data-rel="tag">latest</span></div></header>`;

const footer = `<footer class="foot"><div class="wrap">
<div><strong>snowflake-lite</strong><p>Lock-free unique IDs for Go. MIT licensed.</p></div>
<div><h4>Project</h4><a href="/docs/getting-started/">Documentation</a><a href="/download/">Download</a><a href="/docs/code-map/">Code map</a></div>
<div><h4>Community</h4><a href="/community/">Get involved</a><a href="https://github.com/ysinghc/snowflake-lite/issues">Issues</a><a href="/security/">Report a vulnerability</a></div>
<div><h4>Resources</h4><a href="https://pkg.go.dev/github.com/ysinghc/snowflake-lite/snowflake">pkg.go.dev</a><a href="https://github.com/ysinghc/snowflake-lite">Source</a><a data-rel="notes" href="https://github.com/ysinghc/snowflake-lite/releases">Release notes</a></div>
</div></footer>`;

function page({ title, desc, body, section, extraHead = "", extraBody = "" }) {
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>${esc(title)}</title>
<meta name="description" content="${esc(desc || "Lock-free, time-sortable 64-bit unique IDs for Go.")}">
<link rel="stylesheet" href="/site.css">${extraHead}
</head><body>
<a class="skip" href="#content">Skip to content</a>
${header(section)}
${body}
${footer}
<script src="/site.js" defer></script>${extraBody}
</body></html>`;
}

function docsPage(slugName, { meta, html }) {
  const i = flat.findIndex(([s]) => s === slugName);
  const { html: body, toc } = anchors(html);
  const side = DOCS.map(([group, items]) =>
    `<div class="grp"><h4>${group}</h4>${items.map(([s, t]) => `<a href="/docs/${s}/"${s === slugName ? ' aria-current="page"' : ""}>${t}</a>`).join("")}</div>`).join("");
  const prev = flat[i - 1], next = flat[i + 1];
  const pn = `<nav class="pn" aria-label="Pagination">${prev ? `<a href="/docs/${prev[0]}/"><small>Previous</small>${prev[1]}</a>` : "<span></span>"}${next ? `<a class="r" href="/docs/${next[0]}/"><small>Next</small>${next[1]}</a>` : ""}</nav>`;
  const rail = toc.length > 1 ? `<aside class="toc" aria-label="On this page"><h4>On this page</h4>${toc.map(([id, t]) => `<a href="#${id}">${t}</a>`).join("")}</aside>` : "";
  const content = `<div class="docs">
<details class="side-m"><summary>Documentation menu</summary><nav class="side" aria-label="Documentation">${side}</nav></details>
<nav class="side side-d" aria-label="Documentation">${side}</nav>
<main id="content" class="doc"><p class="crumb">Docs / ${DOCS.find(([, it]) => it.some(([s]) => s === slugName))[0]}</p>
<h1>${meta.title}</h1>${meta.lead ? `<p class="lead">${meta.lead}</p>` : ""}
${body}${pn}
<p class="edit"><a href="https://github.com/ysinghc/snowflake-lite/blob/main/site/content/docs/${slugName}.html">Edit this page on GitHub</a></p></main>
${rail}</div>`;
  return page({ title: `${meta.title} · snowflake-lite`, desc: meta.desc || meta.lead?.replace(/<[^>]+>/g, ""), body: content, section: "docs", extraBody: scripts(meta) });
}

const scripts = (m) => (m.scripts || []).map((u) => `<script src="${u}" defer></script>`).join("");

function write(rel, html) {
  const dest = path.join(out, rel);
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  fs.writeFileSync(dest, html);
}

// Docs pages
for (const [s] of flat) {
  const f = path.join(src, "docs", `${s}.html`);
  if (!fs.existsSync(f)) { console.warn("missing content for docs/" + s); continue; }
  write(`docs/${s}/index.html`, docsPage(s, parse(f)));
}

// Top-level pages (index, download, community, security)
for (const name of ["index", "download", "community", "security"]) {
  const f = path.join(src, `${name}.html`);
  const { meta, html } = parse(f);
  const { html: body } = name === "index" ? { html } : anchors(html);
  const wrapped = name === "index" ? body : `<main id="content" class="wrap prose"><h1>${meta.title}</h1>${meta.lead ? `<p class="lead">${meta.lead}</p>` : ""}${body}</main>`;
  write(name === "index" ? "index.html" : `${name}/index.html`, page({
    title: name === "index" ? "snowflake-lite · lock-free unique IDs for Go" : `${meta.title} · snowflake-lite`,
    desc: meta.desc, body: wrapped, section: name,
    extraBody: scripts(meta),
  }));
}

write("404.html", page({ title: "Not found · snowflake-lite", body: `<main id="content" class="wrap prose"><h1>Page not found</h1><p>That page does not exist. Try the <a href="/docs/getting-started/">documentation</a> or go <a href="/">home</a>.</p></main>`, section: "" }));
console.log("pages built");
