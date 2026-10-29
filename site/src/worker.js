// Serves the static site from the assets binding and fills in release data from GitHub.
// Elements marked data-rel="tag|date|stars|notes" are rewritten on the way out, and
// /api/release(s) expose the same data as JSON. Lookups are edge-cached for 5 minutes,
// so tagging a new release updates every page without a redeploy.

const TTL = 300;
const MODULE = "github.com/ysinghc/snowflake-lite";

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

const day = (iso) => new Date(iso).toISOString().slice(0, 10);
const json = (data) =>
  new Response(JSON.stringify(data), {
    headers: { "content-type": "application/json", "cache-control": `public, max-age=${TTL}`, "access-control-allow-origin": "*" },
  });

async function releaseInfo(repo, ctx) {
  const [rel, meta] = await Promise.all([gh(repo, "/releases/latest", ctx), gh(repo, "", ctx)]);
  return {
    tag: rel ? rel.tag_name : "latest",
    date: rel ? day(rel.published_at) : "",
    url: rel ? rel.html_url : `https://github.com/${repo}/releases`,
    stars: meta ? meta.stargazers_count : null,
  };
}

function rewrite(res, info) {
  return new HTMLRewriter()
    .on("[data-rel]", {
      element(el) {
        const kind = el.getAttribute("data-rel");
        if (kind === "notes") el.setAttribute("href", info.url);
        else if (kind === "tag") el.setInnerContent(info.tag);
        else if (kind === "date" && info.date) el.setInnerContent(info.date);
        else if (kind === "stars" && info.stars !== null) el.setInnerContent(String(info.stars));
      },
    })
    .transform(res);
}

export default {
  async fetch(request, env, ctx) {
    const { pathname } = new URL(request.url);
    const repo = env.GITHUB_REPO;

    if (pathname === "/api/release") return json(await releaseInfo(repo, ctx));
    if (pathname === "/api/releases") {
      const list = (await gh(repo, "/releases?per_page=20", ctx)) || [];
      return json(list.filter((r) => !r.draft).map((r) => ({ tag: r.tag_name, date: day(r.published_at), url: r.html_url, prerelease: r.prerelease })));
    }

    const res = await env.ASSETS.fetch(request);
    if (!(res.headers.get("content-type") || "").includes("text/html")) return res;
    const out = rewrite(res, await releaseInfo(repo, ctx));
    const headers = new Headers(out.headers);
    headers.set("cache-control", `public, max-age=${TTL}`);
    return new Response(out.body, { status: res.status, headers });
  },
};
