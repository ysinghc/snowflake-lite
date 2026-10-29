// Lists releases from the worker's /api/releases endpoint (GitHub data, edge-cached).
const box = document.getElementById("rel-list");
fetch("/api/releases").then((r) => r.json()).then((list) => {
  if (!list.length) { box.innerHTML = '<p class="mute">No releases yet.</p>'; return; }
  const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;");
  box.innerHTML = "<table><tr><th>Version</th><th>Date</th><th></th></tr>" + list.map((r) =>
    `<tr><td><code>${esc(r.tag)}</code>${r.prerelease ? " <span class='pill in'>pre-release</span>" : ""}</td><td>${esc(r.date)}</td><td><a href="${esc(r.url)}">Notes</a></td></tr>`).join("") + "</table>";
}).catch(() => (box.innerHTML = '<p class="mute">Could not load releases. See <a href="https://github.com/ysinghc/snowflake-lite/releases">GitHub</a>.</p>'));
