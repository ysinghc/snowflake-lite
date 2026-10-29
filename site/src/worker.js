// Serves the static site from the assets binding and fills in release data from GitHub.
// Elements marked data-rel="tag|date|notes" are rewritten on the way out, and
// /api/release(s) expose the same data as JSON. Lookups are edge-cached for 5 minutes,
// so tagging a new release updates every page without a redeploy.

const TTL = 300;
const MODULE = "github.com/ysinghc/snowflake-lite";

// Release data comes from the repository's public Atom feed rather than the REST API:
// unauthenticated API calls from shared edge IPs hit GitHub's 60-per-hour limit, while the
// feed does not. A fresh copy lives for TTL seconds; a day-long stale copy covers outages.
async function releases(repo, ctx) {
  const url = `https://github.com/${repo}/releases.atom`;
  const cache = caches.default;
  const fresh = new Request(url);
  const stale = new Request(url + "#stale");
  let xml = await cache.match(fresh).then((r) => r && r.text());
  if (xml === null || xml === undefined) {
    const res = await fetch(url, { headers: { "user-agent": "snowflake-lite-site" } }).catch(() => null);
    if (res && res.ok) {
      xml = await res.text();
      const mk = (age) => new Response(xml, { headers: { "cache-control": `public, max-age=${age}` } });
      ctx.waitUntil(Promise.all([cache.put(fresh, mk(TTL)), cache.put(stale, mk(86400))]));
    } else {
      xml = await cache.match(stale).then((r) => (r ? r.text() : ""));
    }
  }
  return [...xml.matchAll(/<entry>([\s\S]*?)<\/entry>/g)].map((m) => {
    const link = m[1].match(/<link[^>]*href="([^"]+)"/);
    const updated = m[1].match(/<updated>([^<]+)</);
    const tag = link ? decodeURIComponent(link[1].split("/tag/")[1] || "") : "";
    return { tag, date: updated ? day(updated[1]) : "", url: link ? link[1] : "", prerelease: tag.includes("-") };
  }).filter((r) => r.tag);
}

const day = (iso) => new Date(iso).toISOString().slice(0, 10);
const json = (data) =>
  new Response(JSON.stringify(data), {
    headers: { "content-type": "application/json", "cache-control": `public, max-age=${TTL}`, "access-control-allow-origin": "*" },
  });

async function releaseInfo(repo, ctx) {
  const rel = (await releases(repo, ctx)).find((r) => !r.prerelease);
  return {
    tag: rel ? rel.tag : "latest",
    date: rel ? rel.date : "",
    url: rel ? rel.url : `https://github.com/${repo}/releases`,
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
      },
    })
    .transform(res);
}

export default {
  async fetch(request, env, ctx) {
    const { pathname } = new URL(request.url);
    const repo = env.GITHUB_REPO;

    if (pathname === "/api/release") return json(await releaseInfo(repo, ctx));
    if (pathname === "/api/releases") return json((await releases(repo, ctx)).slice(0, 20));

    const res = await env.ASSETS.fetch(request);
    if (!(res.headers.get("content-type") || "").includes("text/html")) return res;
    const out = rewrite(res, await releaseInfo(repo, ctx));
    const headers = new Headers(out.headers);
    headers.set("cache-control", `public, max-age=${TTL}`);
    return new Response(out.body, { status: res.status, headers });
  },
};
