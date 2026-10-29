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
main{max-width:40rem;margin:0 auto;padding:4rem 1.25rem}
h1{font-size:2rem;margin:0 0 .25rem;letter-spacing:-.02em}
p{margin:.5rem 0}.mute{color:var(--mute)}
a{color:var(--acc)}
pre,code{font:14px/1.5 ui-monospace,SFMono-Regular,Menlo,monospace}
pre{background:var(--code);border:1px solid var(--line);border-radius:8px;padding:1rem;overflow-x:auto;margin:1rem 0}
.tag{display:inline-block;border:1px solid var(--line);border-radius:999px;padding:0 .6rem;font-size:.85rem;color:var(--mute)}
ul{padding-left:1.2rem}
footer{margin-top:3rem;padding-top:1rem;border-top:1px solid var(--line);font-size:.9rem;color:var(--mute)}
</style></head><body><main>
<h1>snowflake-lite</h1>
<p class="mute">${desc} One <code>CompareAndSwap</code> per ID, no mutex, and no errors when the clock steps back.</p>
<p><span class="tag">${esc(version)}</span>${date ? ` <span class="mute">released ${esc(date)}</span>` : ""}${stars !== null ? ` <span class="mute">· ${esc(stars)} ★</span>` : ""}</p>
<pre>go get ${esc(mod)}@${esc(version)}</pre>
<pre>import "${esc(mod)}/snowflake"

g := snowflake.MustNew(7) // machine ID 0..1023
id := g.Next()            // strictly ascending, concurrency-safe
fmt.Println(id, id.Time())</pre>
<ul>
<li>41-bit ms timestamp (2024 epoch, ~69 years), 10-bit machine, 12-bit sequence</li>
<li>4096 IDs per ms per machine, up to 1024 machines</li>
<li>IDs exceed 2<sup>53</sup>, so carry them as strings in JSON</li>
</ul>
<p><a href="https://pkg.go.dev/${esc(mod)}/snowflake">Docs</a> ·
<a href="https://github.com/${esc(repo)}">Source</a> ·
<a href="${esc(rel ? rel.html_url : `https://github.com/${repo}/releases`)}">${rel ? "Release notes" : "Releases"}</a></p>
<footer>MIT licensed.</footer>
</main></body></html>`;
}

export default {
  async fetch(request, env, ctx) {
    const { pathname } = new URL(request.url);
    if (pathname !== "/" && pathname !== "/index.html") return new Response("Not found", { status: 404 });
    const repo = env.GITHUB_REPO;
    const [rel, meta] = await Promise.all([gh(repo, "/releases/latest", ctx), gh(repo, "", ctx)]);
    return new Response(page(repo, rel, meta), {
      headers: { "content-type": "text/html; charset=utf-8", "cache-control": `public, max-age=${TTL}` },
    });
  },
};
