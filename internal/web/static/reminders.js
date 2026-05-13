// Tick every reminder's grace-period countdown badge once a second.
// Spans rendered as `<span class="grace" data-end="<unix-ms>">...</span>`
// get their text content rewritten to "⟲ M:SS" until the deadline,
// after which the htmx load-delay trigger replaces the section.
(() => {
  const fmt = (msRemaining) => {
    if (msRemaining <= 0) return '⟲ 0:00';
    const total = Math.ceil(msRemaining / 1000);
    const m = Math.floor(total / 60);
    const s = total % 60;
    return `⟲ ${m}:${s.toString().padStart(2, '0')}`;
  };
  const tick = () => {
    const now = Date.now();
    document.querySelectorAll('#reminders .grace[data-end]').forEach((el) => {
      const end = parseInt(el.dataset.end, 10);
      if (!Number.isFinite(end)) return;
      el.textContent = fmt(end - now);
    });
  };
  tick();
  setInterval(tick, 1000);
})();
