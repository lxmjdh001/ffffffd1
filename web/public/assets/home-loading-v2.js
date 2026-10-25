// Small, independent bootstrap: run before the console bundle is ready.
(() => {
  const isHome = () => !location.hash || location.hash === '#' || /^#\/welcome(?:\?|$)/.test(location.hash);
  if (!isHome()) return;
  // Fetch public configuration in parallel with the much larger app bundle.
  window.__d1HomeConfig = fetch('/api/config', { credentials: 'same-origin' })
    .then(response => response.ok ? response.json() : undefined).catch(() => undefined);
  const root = document.getElementById('root');
  const overlay = document.getElementById('home-loading');
  if (!root || !overlay) return;
  const started = Date.now();
  const oldInert = root.inert;
  let stopped = false;
  let readyTimer;
  const finish = () => {
    if (stopped) return;
    stopped = true;
    clearTimeout(readyTimer);
    clearTimeout(timeout);
    document.documentElement.removeAttribute('data-home-loading');
    overlay.hidden = true;
    root.inert = oldInert;
    root.removeAttribute('aria-busy');
    window.removeEventListener('d1:home-ready', ready);
    window.removeEventListener('hashchange', navigate);
  };
  const ready = () => { readyTimer = setTimeout(finish, Math.max(0, 1000 - (Date.now() - started))); };
  const navigate = () => { if (!isHome()) finish(); };
  overlay.querySelector('[data-loading-label], span').textContent = 'Loading...';
  document.documentElement.setAttribute('data-home-loading', '');
  root.inert = true;
  root.setAttribute('aria-busy', 'true');
  overlay.hidden = false;
  const timeout = setTimeout(finish, 2000);
  window.addEventListener('d1:home-ready', ready);
  window.addEventListener('hashchange', navigate);
})();
