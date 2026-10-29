// Renders the project page from the latest GitHub release. The release lookup is
// edge-cached for 5 minutes, so a new tag shows up without redeploying.

const TTL = 300;

const esc = (s) =>
  String(s).replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

async function gh(repo, path, ctx) {
  const url = `https://api.github.com/repos/${repo}${path}`;
  const cache = caches.default;
  const key = new Request(url);
  let res = await cache.match(key);
  if (!res) {
    res = await fetch(url, { headers: { "user-agent": "snowflake-lite-site", accept: "application/vnd.github+json" } });
    if (!res.ok) return null;
    res = new Response(res.body, res);
    res.headers.set("cache-control", `public, max-age=${TTL}`);
    ctx.waitUntil(cache.put(key, res.clone()));
  }
  return res.json();
}

function page(repo, rel, meta) {
  const mod = `github.com/${repo}`;
  const version = rel ? rel.tag_name : "latest";
  const date = rel ? new Date(rel.published_at).toISOString().slice(0, 10) : "";
  const stars = meta ? meta.stargazers_count : null;
  const desc = "Lock-free, time-sortable 64-bit unique IDs for Go.";
  return `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>snowflake-lite</title>
<meta name="description" content="${desc}">
<style>
:root{--bg:#fff;--fg:#17181c;--mute:#666b75;--line:#e4e6ea;--code:#f5f6f8;--acc:#2563eb;color-scheme:light dark}
@media(prefers-color-scheme:dark){:root{--bg:#0e0f12;--fg:#e8e9ec;--mute:#9096a1;--line:#24262c;--code:#16181d;--acc:#7aa2ff}}
body{margin:0;background:var(--bg);color:var(--fg);font:16px/1.6 ui-sans-serif,system-ui,sans-serif}
main{max-width:42rem;margin:0 auto;padding:4rem 1.25rem}
h1{font-size:2rem;margin:0 0 .25rem;letter-spacing:-.02em}
p{margin:.5rem 0}.mute{color:var(--mute)}
a{color:var(--acc)}
pre,code{font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}
pre{background:var(--code);border:1px solid var(--line);border-radius:8px;padding:1rem;overflow-x:auto;margin:1rem 0}
.tag{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:0 .6rem;font-size:.85rem;color:var(--mute)}
ul{padding-left:1.2rem}
h2{font-size:1.15rem;margin:2.5rem 0 .5rem}
.lead{font-size:1.1rem;color:var(--mute)}
.links a{margin-right:1rem}
.play{border:1px solid var(--line);border-radius:10px;padding:1rem;margin-top:.75rem}
.row{display:flex;flex-wrap:wrap;gap:.5rem;align-items:center;margin:.5rem 0}
input,button{font:inherit;padding:.4rem .7rem;border:1px solid var(--line);border-radius:6px;background:var(--bg);color:var(--fg)}
input{min-width:0}#machine{width:5rem}#decode-in{flex:1;font-family:ui-monospace,monospace}
button{cursor:pointer}button:hover{border-color:var(--acc)}
#ids{list-style:none;padding:0;margin:.5rem 0;max-height:11rem;overflow:auto;font:14px ui-monospace,monospace}
#ids li{padding:.15rem .4rem;border-radius:4px;cursor:pointer}#ids li:hover,#ids li:focus{background:var(--code);outline:0}
.bits{display:flex;font:12px ui-monospace,monospace;word-break:break-all;margin:.5rem 0;border-radius:6px;overflow:hidden}
.seg{padding:.4rem .3rem;color:#fff;text-align:center}
.u{background:#6b7280;flex:1}.t{background:#2563eb;flex:41}.m{background:#059669;flex:10}.s{background:#d97706;flex:12}
dl{display:grid;grid-template-columns:max-content 1fr;gap:.1rem 1rem;margin:.5rem 0;font-size:.9rem}dt{color:var(--mute)}dd{margin:0;font-family:ui-monospace,monospace;word-break:break-all}
footer{margin-top:3rem;padding-top:1rem;border-top:1px solid var(--line);font-size:.9rem;color:var(--mute)}
</style></head><body><main>
<h1>snowflake-lite</h1>
<p class="lead">${desc} One <code>CompareAndSwap</code> per ID, no mutex, and no errors when the clock steps back.</p>
<p><span class="tag">${esc(version)}</span>${date ? ` <span class="mute">released ${esc(date)}</span>` : ""}${stars !== null ? ` <span class="mute">· ${esc(stars)} ★</span>` : ""}</p>
<pre>go get ${esc(mod)}@${esc(version)}</pre>
<p class="links"><a href="https://pkg.go.dev/${esc(mod)}/snowflake">pkg.go.dev</a>
<a href="https://github.com/${esc(repo)}">Source</a>
<a href="${esc(rel ? rel.html_url : `https://github.com/${repo}/releases`)}">${rel ? "Release notes" : "Releases"}</a></p>

<h2>Try it</h2>
<p class="mute">This runs the actual Go package, compiled to WebAssembly in your browser. <span id="status">Loading…</span></p>
<section id="play" class="play">
<div class="row">
<label>Machine ID <input id="machine" type="number" min="0" max="1023" value="7"></label>
<button data-n="1">Generate 1</button><button data-n="10">10</button><button data-n="100">100</button>
</div>
<ol id="ids"></ol>
<div id="decoded"></div>
<div class="row">
<input id="decode-in" placeholder="Paste an ID to decode" inputmode="numeric" aria-label="ID to decode">
<button id="decode-btn">Decode</button>
</div>
<div class="row"><button id="bench">Benchmark 1M IDs</button><span id="bench-out" class="mute"></span></div>
</section>

<h2>Use it</h2>
<pre>import "${esc(mod)}/snowflake"

g := snowflake.MustNew(7) // machine ID 0..1023
id := g.Next()            // strictly ascending, concurrency-safe
fmt.Println(id, id.Time())</pre>

<h2>Layout</h2>
<div class="bits"><span class="seg u">1</span><span class="seg t">41-bit timestamp</span><span class="seg m">10</span><span class="seg s">12</span></div>
<ul>
<li>Timestamp: milliseconds from a 2024 epoch, about 69 years of headroom</li>
<li>Machine: up to 1024 generators. Sequence: 4096 IDs per ms per machine</li>
<li>IDs exceed 2<sup>53</sup>, so carry them as strings in JSON</li>
</ul>
<footer>MIT licensed · <a href="https://github.com/${esc(repo)}/blob/main/LICENSE">License</a></footer>
<script src="/wasm_exec.js"></script><script src="/playground.js"></script>
</main></body></html>`;
}

export default {
  async fetch(request, env, ctx) {
    const { pathname } = new URL(request.url);
    if (pathname !== "/" && pathname !== "/index.html") return env.ASSETS.fetch(request);
    const repo = env.GITHUB_REPO;
    const [rel, meta] = await Promise.all([gh(repo, "/releases/latest", ctx), gh(repo, "", ctx)]);
    return new Response(page(repo, rel, meta), {
      headers: { "content-type": "text/html; charset=utf-8", "cache-control": `public, max-age=${TTL}` },
    });
  },
};
