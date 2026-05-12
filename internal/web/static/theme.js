(function () {
  const btn = document.getElementById("theme-toggle");
  if (!btn) return;
  const render = () => {
    const t = document.documentElement.getAttribute("data-theme");
    btn.textContent = t === "light" ? "☾" : "☀";
    btn.setAttribute(
      "aria-label",
      t === "light" ? "Switch to dark theme" : "Switch to light theme",
    );
  };
  btn.addEventListener("click", () => {
    const cur = document.documentElement.getAttribute("data-theme");
    const next = cur === "light" ? "dark" : "light";
    document.documentElement.setAttribute("data-theme", next);
    try { localStorage.setItem("theme", next); } catch (_) {}
    render();
  });
  render();
})();
