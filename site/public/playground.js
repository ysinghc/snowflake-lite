// Drives the playground with the real Go package compiled to WebAssembly.
const $ = (id) => document.getElementById(id);
const out = $("ids"), dec = $("decoded"), status = $("status");
let ready = false;

window.onSnowReady = () => {
  ready = true;
  status.textContent = "Go package loaded (WebAssembly)";
  $("play").classList.add("ready");
  generate(5);
};

(async () => {
  try {
    const go = new Go();
    const r = await WebAssembly.instantiateStreaming(fetch("/snowflake.wasm"), go.importObject);
    go.run(r.instance);
  } catch (e) {
    status.textContent = "Could not load WebAssembly: " + e.message;
  }
})();

function machine() {
  const m = parseInt($("machine").value, 10);
  const r = snowNew(Number.isNaN(m) ? -1 : m);
  if (r.error) { status.textContent = r.error; return false; }
  status.textContent = "Machine " + r.machineID;
  return true;
}

function show(id) {
  const d = snowDecode(id);
  if (d.error) { dec.innerHTML = '<p class="mute">' + d.error + "</p>"; return; }
  const b = d.bits;
  const seg = (s, c, l) => `<span class="seg ${c}" title="${l}">${s}</span>`;
  dec.innerHTML =
    `<div class="bits">${seg(b[0], "u", "unused")}${seg(b.slice(1, 42), "t", "timestamp")}${seg(b.slice(42, 52), "m", "machine")}${seg(b.slice(52), "s", "sequence")}</div>` +
    `<dl><dt>time</dt><dd>${d.timestamp}</dd><dt>machine</dt><dd>${d.machineID}</dd><dt>sequence</dt><dd>${d.sequence}</dd><dt>ms since epoch</dt><dd>${d.millisSinceEpoch}</dd></dl>`;
  $("decode-in").value = d.id;
}

function generate(n) {
  if (!ready || !machine()) return;
  const ids = snowNextBatch(n);
  out.innerHTML = "";
  for (const id of ids) {
    const li = document.createElement("li");
    li.textContent = id;
    li.tabIndex = 0;
    li.onclick = () => show(id);
    li.onkeydown = (e) => e.key === "Enter" && show(id);
    out.append(li);
  }
  show(ids[ids.length - 1]);
}

document.querySelectorAll("[data-n]").forEach((b) => (b.onclick = () => generate(+b.dataset.n)));
$("decode-btn").onclick = () => ready && show($("decode-in").value.trim());
$("bench").onclick = () => {
  if (!ready || !machine()) return;
  $("bench-out").textContent = "running…";
  setTimeout(() => {
    const r = snowBench(1000000);
    $("bench-out").textContent =
      `${r.count.toLocaleString()} IDs in ${r.elapsedMs.toFixed(0)} ms (${Math.round(r.rate).toLocaleString()}/s), strictly ascending: ${r.monotonic}`;
  }, 20);
};
