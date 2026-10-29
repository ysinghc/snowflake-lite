// Copy buttons for code blocks and "on this page" highlighting.
document.querySelectorAll("pre").forEach((pre) => {
  if (pre.closest("#play") || pre.dataset.nocopy !== undefined) return;
  const b = document.createElement("button");
  b.className = "copy"; b.type = "button"; b.textContent = "Copy";
  b.onclick = async () => {
    try { await navigator.clipboard.writeText(pre.innerText.replace(/\nCopy$/, "")); b.textContent = "Copied"; }
    catch { b.textContent = "Press Ctrl+C"; }
    setTimeout(() => (b.textContent = "Copy"), 1500);
  };
  pre.append(b);
});
const links = [...document.querySelectorAll(".toc a")];
if (links.length && "IntersectionObserver" in window) {
  const map = new Map(links.map((a) => [a.getAttribute("href").slice(1), a]));
  const io = new IntersectionObserver((es) => es.forEach((e) => {
    if (e.isIntersecting) { links.forEach((l) => l.classList.remove("on")); map.get(e.target.id)?.classList.add("on"); }
  }), { rootMargin: "-10% 0px -80% 0px" });
  map.forEach((_, id) => { const el = document.getElementById(id); if (el) io.observe(el); });
}
