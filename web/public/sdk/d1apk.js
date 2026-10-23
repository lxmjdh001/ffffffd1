/* D1 Apk WebView event client. Never put your server signing secret in this file. */
(function (global) {
  'use strict';
  function Client(options) {
    if (!options || !/^[a-f0-9]{32}$/.test(options.appId)) throw new Error('D1 Apk: invalid appId');
    this.endpoint = (options.baseURL || 'https://d1apk.com').replace(/\/$/, '') + '/api/sdk/apps/' + options.appId + '/events';
    this.key = 'd1apk:' + options.appId;
    this.storage = options.storage || global.localStorage;
    const incoming = new URL(global.location.href).searchParams.get('d1_token');
    if (incoming && /^[a-f0-9]{32}\.[a-f0-9]{64}$/.test(incoming)) {
      this.storage.setItem(this.key + ':attribution', JSON.stringify({ token: incoming, until: Date.now() + 7 * 86400000 }));
      // Do not leak the bearer attribution token into subsequent referrers.
      const clean = new URL(global.location.href); clean.searchParams.delete('d1_token');
      global.history.replaceState(null, '', clean);
    }
  }
  Client.prototype.getToken = function () {
    try { const x = JSON.parse(this.storage.getItem(this.key + ':attribution')); return x && x.until > Date.now() ? x.token : null; } catch (_) { return null; }
  };
  Client.prototype.track = async function (event, options) {
    options = options || {};
    if (event !== 'app_open' && event !== 'register') throw new Error('D1 Apk: unsupported event');
    const token = this.getToken();
    if (!token) return { accepted: false, reason: 'no_attribution' };
    if (options.consent !== true) return { accepted: false, reason: 'consent_required' };
    const key = this.key + ':' + token.slice(0, 32) + ':' + event;
    let saved;
    try { saved = JSON.parse(this.storage.getItem(key)); } catch (_) {}
    if (saved && saved.accepted) return { accepted: true, duplicate: true, result: saved.result };
    const payload = saved && saved.payload || {
      attribution_token: token, event: event,
      event_id: options.eventId || global.crypto.randomUUID(),
      event_time: Math.floor(Date.now() / 1000), consent: true,
      page_url: global.location.origin + global.location.pathname,
      external_id: options.externalId || ''
    };
    this.storage.setItem(key, JSON.stringify({ payload: payload }));
    const response = await global.fetch(this.endpoint, { method: 'POST', credentials: 'omit', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(payload) });
    let result; try { result = await response.json(); } catch (_) { result = {}; }
    if (!response.ok) {
      const error = new Error(result.error || ('D1 Apk HTTP ' + response.status)); error.status = response.status;
      error.retryable = response.status === 409 || response.status === 429 || response.status >= 500;
      throw error;
    }
    this.storage.setItem(key, JSON.stringify({ accepted: true, result: result }));
    return { accepted: true, result: result };
  };
  global.D1Apk = Client;
})(window);
