// Headless verification of the compiled snowflake.wasm through the real
// wasm_exec.js glue — the same artifacts the browser loads. Run with:
//   node web/verify_wasm.mjs
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const dir = path.dirname(fileURLToPath(import.meta.url));

// wasm_exec.js is a classic script that assigns globalThis.Go; load it as text
// and eval in this scope (it references globalThis internally).
const glue = fs.readFileSync(path.join(dir, "wasm_exec.js"), "utf8");
new Function(glue)();

let failures = 0;
const check = (cond, msg) => {
  if (!cond) { failures++; console.error("  FAIL:", msg); }
  else console.log("  ok  :", msg);
};

globalThis.onSnowReady = () => {
  try {
    runTests();
  } catch (e) {
    console.error("harness error:", e);
    process.exit(1);
  }
  process.exit(failures === 0 ? 0 : 1);
};

function runTests() {
  console.log("== snowNext: uniqueness + strict monotonicity (100k) ==");
  const N = 100_000;
  const seen = new Set();
  let prev = -1n, mono = true;
  check(typeof snowNext() === "string", "returns a string");
  for (let i = 0; i < N; i++) {
    const s = snowNext();
    const v = BigInt(s);
    if (v <= prev) mono = false;
    prev = v;
    seen.add(s);
  }
  check(seen.size === N, `all ${N} IDs unique (got ${seen.size})`);
  check(mono, "strictly ascending across 100k IDs");

  console.log("== snowDecode: round-trip of a fresh ID ==");
  const id = snowNext();
  const d = snowDecode(id);
  check(!d.error, "decode has no error");
  check(d.id === id, "decoded id echoes input");
  check(typeof d.bits === "string" && d.bits.length === 64, `bits is 64 chars (got ${d.bits && d.bits.length})`);
  check(d.machineID === 1, `machine defaults to 1 (got ${d.machineID})`);
  check(d.sequence >= 0 && d.sequence <= 4095, `sequence in range (got ${d.sequence})`);
  // recompose from bit string and compare
  check(BigInt("0b" + d.bits) === BigInt(id), "bit string recomposes to the ID");

  console.log("== snowNew: machine ID bounds ==");
  check(snowNew(1023).machineID === 1023, "machine 1023 accepted");
  check(!!snowNew(1024).error, "machine 1024 rejected");
  check(!!snowNew(-1).error, "machine -1 rejected");
  snowNew(7);
  check(snowDecode(snowNext()).machineID === 7, "new IDs carry machine 7");

  console.log("== snowNextBatch: shape + clamp ==");
  const batch = snowNextBatch(5);
  check(Array.isArray(batch) && batch.length === 5, `batch of 5 (got ${batch.length})`);
  check(new Set(batch).size === 5, "batch IDs unique");
  check(snowNextBatch(1_000_000).length === 200_000, "batch clamped to 200k");
  check(snowNextBatch(-3).length === 0, "negative batch -> empty");

  console.log("== snowBench: shape + guarantees ==");
  const r = snowBench(500_000);
  check(r.count === 500_000, `count echoed (${r.count})`);
  check(r.monotonic === true, "bench reports monotonic");
  check(typeof r.rate === "number" && r.rate > 0, `positive rate (${Math.round(r.rate).toLocaleString()}/s)`);
  check(typeof r.lastID === "string", "lastID is a string");

  console.log(failures === 0 ? "\nALL WASM CHECKS PASSED" : `\n${failures} CHECK(S) FAILED`);
}

const go = new globalThis.Go();
const bytes = fs.readFileSync(path.join(dir, "snowflake.wasm"));
const { instance } = await WebAssembly.instantiate(bytes, go.importObject);
go.run(instance); // runs main(), which invokes onSnowReady synchronously
