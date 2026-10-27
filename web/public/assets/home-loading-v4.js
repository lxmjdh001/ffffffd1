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
    clearTimeout(slowTimer);
    document.documentElement.removeAttribute('data-home-loading');
    overlay.hidden = true;
    root.inert = oldInert;
    root.removeAttribute('aria-busy');
    window.removeEventListener('d1:home-ready', ready);
    window.removeEventListener('hashchange', navigate);
    window.removeEventListener('d1:home-error', showRetry);
  };
  const ready = () => { readyTimer = setTimeout(finish, Math.max(0, 1000 - (Date.now() - started))); };
  const navigate = () => { if (!isHome()) finish(); };
  overlay.querySelector('[data-loading-label], span').textContent = 'Loading...';
  document.documentElement.setAttribute('data-home-loading', '');
  root.inert = true;
  root.setAttribute('aria-busy', 'true');
  overlay.hidden = false;
  // A slow connection is not readiness. Keep the loader until the usable page
  // signals completion; offer recovery instead of revealing an unfinished page.
  const showRetry = () => {
    if (stopped || overlay.querySelector('button')) return;
    overlay.querySelector('[data-loading-label], span').textContent = 'Still loading...';
    const retry = document.createElement('button');
    retry.type = 'button'; retry.className = 'home-loading-retry';
    retry.textContent = 'Retry'; retry.onclick = () => location.reload();
    (overlay.querySelector('.home-loading-card') || overlay).appendChild(retry);
  };
  const slowTimer = setTimeout(showRetry, 15000);
  window.addEventListener('d1:home-error', showRetry);
  window.addEventListener('d1:home-ready', ready);
  window.addEventListener('hashchange', navigate);
})();
