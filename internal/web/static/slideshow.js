(() => {
  const root = document.getElementById('photos');
  if (!root) return;
  const slides = root.querySelectorAll('.slide');
  if (slides.length < 2) return;
  const interval = (parseInt(root.dataset.interval, 10) || 8) * 1000;
  let i = 0;
  setInterval(() => {
    slides[i].classList.remove('active');
    i = (i + 1) % slides.length;
    slides[i].classList.add('active');
  }, interval);
})();
