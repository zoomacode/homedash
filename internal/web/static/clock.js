(() => {
  const dateEl = document.querySelector('#clock .date');
  const timeEl = document.querySelector('#clock .time');
  if (!dateEl || !timeEl) return;
  function tick() {
    const now = new Date();
    timeEl.textContent = now.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
    dateEl.textContent = now.toLocaleDateString([], { weekday: 'short', day: '2-digit', month: 'short', year: 'numeric' });
  }
  tick();
  setInterval(tick, 1000);
})();
