(() => {
  const root = document.getElementById('photos');
  if (!root) return;
  const slides = root.querySelectorAll('.slide');
  if (slides.length < 1) return;

  const interval = (parseInt(root.dataset.interval, 10) || 900) * 1000;
  let i = 0;
  let timer = null;

  const showActive = () => {
    slides.forEach((s, idx) => s.classList.toggle('active', idx === i));
  };

  const advance = () => {
    if (slides.length < 2) return;
    i = (i + 1) % slides.length;
    showActive();
  };

  const startTimer = () => {
    if (timer) clearInterval(timer);
    if (slides.length < 2) return;
    timer = setInterval(advance, interval);
  };

  showActive();
  startTimer();

  // Manual skip resets the auto-rotate timer so the new slide stays
  // visible for the full interval.
  document.getElementById('photos-next')?.addEventListener('click', () => {
    advance();
    startTimer();
  });
})();
