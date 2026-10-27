import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { resolve } from 'node:path';
import { describe, expect, it, vi } from 'vitest';

const worker = readFileSync(resolve(process.cwd(), '../internal/platform/templates/pwa-sw.js'), 'utf8');
const client = readFileSync(resolve(process.cwd(), '../internal/platform/templates/pwa-client.js'), 'utf8');
const base = 'https://d1.example/p/0123456789abcdef/';

function workerContext(workerBase = base, notificationSupport?: { maxActions?: number; permission?: string }) {
  const listeners = new Map<string, (event: any) => void>();
  const fetcher = vi.fn().mockResolvedValue(new Response('{}'));
  const show = vi.fn().mockResolvedValue(undefined);
  const open = vi.fn().mockResolvedValue(undefined);
  const focus = vi.fn().mockResolvedValue(undefined);
  const matchAll = vi.fn().mockResolvedValue([{ url: workerBase + 'launch', focus }]);
  const self = {
    location: { href: workerBase + 'sw.js' },
    addEventListener: (name: string, handler: (event: any) => void) => listeners.set(name, handler),
    registration: { showNotification: show },
    clients: { claim: vi.fn(), matchAll, openWindow: open },
    skipWaiting: vi.fn(),
  };
  runInNewContext(worker, { self, URL, Response, fetch: fetcher, ...(notificationSupport ? { Notification: notificationSupport } : {}) });
  const dispatch = async (name: string, values: Record<string, any>) => {
    let pending: Promise<any> | undefined;
    listeners.get(name)!({ ...values, waitUntil: (value: Promise<any>) => { pending = value; } });
    await pending;
  };
  return { listeners, fetcher, show, open, focus, matchAll, dispatch };
}

describe('PWA service worker boundaries and notifications', () => {
  it('does not intercept console, API, or another app navigation', () => {
    const { listeners } = workerContext();
    for (const url of ['https://d1.example/api/me', 'https://d1.example/', 'https://d1.example/p/another/launch', 'https://example.com/']) {
      const respondWith = vi.fn();
      listeners.get('fetch')!({ request: { mode: 'navigate', url }, respondWith });
      expect(respondWith).not.toHaveBeenCalled();
    }
  });

  it('displays a notification before acknowledging it, uses a stable tag and safe app icon', async () => {
    const w = workerContext();
    const payload = { title: 'Hello', body: 'A new message', icon: 'javascript:alert(1)', url: 'javascript:alert(1)', delivery_id: 'delivery-one', event_token: 'event-capability' };
    await w.dispatch('push', { data: { json: () => payload } });
    await w.dispatch('push', { data: { json: () => payload } });
    const first = w.show.mock.calls[0];
    expect(first[0]).toBe('Hello');
    expect(first[1].icon).toBe('https://d1.example/pwa-template-assets/push-google.png');
    expect(first[1].data.url).toBe(base + 'launch');
    expect(first[1].tag).toBe(w.show.mock.calls[1][1].tag);
    expect(first[1].renotify).toBe(false);
    expect(w.show.mock.invocationCallOrder[0]).toBeLessThan(w.fetcher.mock.invocationCallOrder[0]);
    const [url, request] = w.fetcher.mock.calls[0];
    expect(String(url)).toBe(base + 'event');
    expect(JSON.parse(request.body)).toEqual({ delivery_id: 'delivery-one', token: 'event-capability', event: 'displayed' });
    expect(request.credentials).toBe('omit');
  });

  it('focuses the installed app and reports clicks using its delivery capability', async () => {
    const w = workerContext();
    const close = vi.fn();
    await w.dispatch('notificationclick', { notification: { close, data: { url: base + 'launch', delivery_id: 'one', event_token: 'secret' } } });
    expect(close).toHaveBeenCalledOnce();
    expect(w.focus).toHaveBeenCalledOnce();
    expect(w.open).not.toHaveBeenCalled();
    expect(w.matchAll).toHaveBeenCalledWith({ type: 'window', includeUncontrolled: true });
    expect(JSON.parse(w.fetcher.mock.calls[0][1].body).event).toBe('clicked');
  });

  it('does not claim a display when the operating system rejects the notification', async () => {
    const w = workerContext();
    w.show.mockRejectedValueOnce(new Error('permission denied'));
    await expect(w.dispatch('push', { data: { json: () => ({ title: 'A', delivery_id: 'one', event_token: 'secret' }) } })).rejects.toThrow('permission denied');
    expect(w.fetcher).not.toHaveBeenCalled();
  });

  it.each([null, [], 'invalid', 42, true].map(value => ({ value })))('displays a default notification for a non-object payload $value without reporting a delivery', async ({ value }) => {
    const w = workerContext();
    await w.dispatch('push', { data: { json: () => value } });
    expect(w.show).toHaveBeenCalledExactlyOnceWith('New update', expect.objectContaining({ body: '', data: expect.objectContaining({ url: base + 'launch' }) }));
    expect(w.fetcher).not.toHaveBeenCalled();
  });

  it('displays a default notification for malformed JSON without reporting a delivery', async () => {
    const w = workerContext();
    await w.dispatch('push', { data: { json: () => { throw new SyntaxError('malformed JSON'); } } });
    expect(w.show).toHaveBeenCalledExactlyOnceWith('New update', expect.objectContaining({ body: '', data: expect.objectContaining({ url: base + 'launch' }) }));
    expect(w.fetcher).not.toHaveBeenCalled();
  });

  it('displays a default notification for a push without a payload', async () => {
    const w = workerContext();
    await w.dispatch('push', {});
    expect(w.show).toHaveBeenCalledExactlyOnceWith('New update', expect.objectContaining({ body: '', data: expect.objectContaining({ url: base + 'launch' }) }));
    expect(w.fetcher).not.toHaveBeenCalled();
  });

  it('displays every push event with the same replacement tag and no repeated alert request after a restart', async () => {
    const first = workerContext();
    const payload = { title: 'Only once', delivery_id: 'a'.repeat(32), event_token: 'proof' };
    await first.dispatch('push', { data: { json: () => payload } });
    expect(first.show).toHaveBeenCalledOnce();
    const restarted = workerContext();
    await restarted.dispatch('push', { data: { json: () => payload } });
    expect(restarted.show).toHaveBeenCalledOnce();
    expect(restarted.show.mock.calls[0][1]).toMatchObject({ tag: first.show.mock.calls[0][1].tag, renotify: false });
    expect(JSON.parse(restarted.fetcher.mock.calls[0][1].body).event).toBe('displayed');
  });

  it.each([
    'https://d1.example/d/example',
    'https://d1.example/s/example?from=push',
    'https://d1.example/c/example#content',
    'https://landing.example/offer?campaign=one#start',
  ])('opens the configured HTTPS destination %s without focusing an unrelated app window', async target => {
    const w = workerContext();
    await w.dispatch('push', { data: { json: () => ({ title: 'A', url: target }) } });
    expect(w.show.mock.calls[0][1].data.url).toBe(target);
    await w.dispatch('notificationclick', { notification: { close: vi.fn(), data: w.show.mock.calls[0][1].data } });
    expect(w.open).toHaveBeenCalledExactlyOnceWith(target);
    expect(w.focus).not.toHaveBeenCalled();
  });

  it('focuses an exact configured business URL even when it is outside the worker scope', async () => {
    const w = workerContext();
    const target = 'https://d1.example/s/example';
    const matchingFocus = vi.fn().mockResolvedValue(undefined);
    w.matchAll.mockResolvedValue([{ url: base + 'launch', focus: w.focus }, { url: target, focus: matchingFocus }]);
    await w.dispatch('notificationclick', { notification: { close: vi.fn(), data: { url: target } } });
    expect(matchingFocus).toHaveBeenCalledOnce();
    expect(w.focus).not.toHaveBeenCalled();
    expect(w.open).not.toHaveBeenCalled();
  });

  it.each(['javascript:alert(1)', 'data:text/html,hello', 'http://landing.example/', 'https://user:password@landing.example/', 'https://[invalid', ''])('falls back to the app launch page for unsafe or empty destination %s', async target => {
    const w = workerContext();
    await w.dispatch('notificationclick', { notification: { close: vi.fn(), data: { url: target } } });
    expect(w.focus).toHaveBeenCalledOnce();
    expect(w.open).not.toHaveBeenCalled();
  });

  it('allows only the exact loopback HTTP origin during development', async () => {
    const devBase = 'http://127.0.0.1:18088/p/0123456789abcdef/';
    const w = workerContext(devBase);
    const target = 'http://127.0.0.1:18088/s/example';
    await w.dispatch('notificationclick', { notification: { close: vi.fn(), data: { url: target } } });
    expect(w.open).toHaveBeenCalledExactlyOnceWith(target);
    for (const unsafe of ['http://127.0.0.1:18089/s/example', 'http://127.0.0.1.attacker.example:18088/s/example']) {
      await w.dispatch('notificationclick', { notification: { close: vi.fn(), data: { url: unsafe } } });
    }
    expect(w.open).toHaveBeenCalledOnce();
    expect(w.focus).toHaveBeenCalledTimes(2);
  });

  it('displays configured HTTPS notification icons and large images', async () => {
    const w = workerContext();
    const icon = 'https://cdn.example/notification-icon.png';
    const image = 'https://d1.example/pwa-push-assets/0123456789abcdef.png';
    await w.dispatch('push', { data: { json: () => ({ title: 'News', icon, image }) } });
    expect(w.show).toHaveBeenCalledExactlyOnceWith('News', expect.objectContaining({ icon: 'https://d1.example/pwa-template-assets/push-google.png', image, badge: 'https://d1.example/pwa-template-assets/push-google.png' }));
  });

  it.each(['javascript:alert(1)', 'data:image/png;base64,AA', 'http://cdn.example/image.png', 'https://user:secret@cdn.example/image.png', 'https://[invalid', ''])('rejects unsafe notification image and icon URL %s', async image => {
    const w = workerContext();
    await w.dispatch('push', { data: { json: () => ({ title: 'Safe defaults', icon: image, image }) } });
    const options = w.show.mock.calls[0][1];
    expect(options.icon).toBe('https://d1.example/pwa-template-assets/push-google.png');
    expect(options).not.toHaveProperty('image');
  });

  it('accepts uploaded image paths and limits HTTP images to the exact development origin', async () => {
    const devBase = 'http://127.0.0.1:18088/p/0123456789abcdef/';
    const w = workerContext(devBase);
    await w.dispatch('push', { data: { json: () => ({ icon: '/pwa-push-assets/icon.png', image: 'http://127.0.0.1:18088/pwa-push-assets/banner.png' }) } });
    expect(w.show.mock.calls[0][1]).toMatchObject({ icon: 'http://127.0.0.1:18088/pwa-template-assets/push-google.png', image: 'http://127.0.0.1:18088/pwa-push-assets/banner.png' });
    for (const image of ['http://127.0.0.1:18089/image.png', 'http://127.0.0.1.attacker.example:18088/image.png', 'http://localhost:18088/image.png']) {
      await w.dispatch('push', { data: { json: () => ({ image }) } });
      expect(w.show.mock.calls.at(-1)![1]).not.toHaveProperty('image');
    }
  });

  it('uses separate destinations for both notification action buttons and reports one click per event', async () => {
    const w = workerContext();
    const first = 'https://landing.example/offer';
    const second = 'https://d1.example/d/example?from=push-button';
    await w.dispatch('push', { data: { json: () => ({ title: 'Offer', url: base + 'launch', delivery_id: 'one', event_token: 'proof', actions: [{ action: 'view', title: 'View offer', url: first }, { action: 'download', title: 'Download', url: second }] }) } });
    const options = w.show.mock.calls[0][1];
    expect(options.actions).toEqual([{ action: 'view', title: 'View offer' }, { action: 'download', title: 'Download' }]);
    expect(options.data.actions).toEqual({ view: first, download: second });
    for (const [action, target] of [['view', first], ['download', second]]) {
      w.fetcher.mockClear();
      w.open.mockClear();
      await w.dispatch('notificationclick', { action, notification: { close: vi.fn(), data: options.data } });
      expect(w.open).toHaveBeenCalledExactlyOnceWith(target);
      expect(w.fetcher).toHaveBeenCalledOnce();
      expect(JSON.parse(w.fetcher.mock.calls[0][1].body)).toEqual({ delivery_id: 'one', token: 'proof', event: 'clicked' });
    }
  });

  it.each(['', 'unknown', 'toString'])('uses the main notification URL for body or unknown action %s', async action => {
    const w = workerContext();
    const main = 'https://landing.example/main';
    await w.dispatch('notificationclick', { action, notification: { close: vi.fn(), data: { url: main, actions: { view: 'https://landing.example/button' } } } });
    expect(w.open).toHaveBeenCalledExactlyOnceWith(main);
  });

  it('normalizes unsafe button URLs to the main URL and validates them again on click', async () => {
    const w = workerContext();
    const main = 'https://landing.example/main';
    await w.dispatch('push', { data: { json: () => ({ url: main, actions: [{ action: 'unsafe', title: 'Open', url: 'javascript:alert(1)' }] }) } });
    expect(w.show.mock.calls[0][1].data.actions).toEqual({ unsafe: main });
    await w.dispatch('notificationclick', { action: 'unsafe', notification: { close: vi.fn(), data: { url: main, actions: { unsafe: 'data:text/html,unsafe' } } } });
    expect(w.open).toHaveBeenCalledExactlyOnceWith(main);
  });

  it('keeps at most two valid unique buttons and ignores malformed actions', async () => {
    const w = workerContext();
    await w.dispatch('push', { data: { json: () => ({ actions: [null, { action: '', title: 'Empty' }, { action: 'missing-title' }, { action: 'first', title: 'First', url: 'https://one.example/' }, { action: 'first', title: 'Duplicate', url: 'https://duplicate.example/' }, { action: 'second', title: 'Second', url: 'https://two.example/' }, { action: 'third', title: 'Third', url: 'https://three.example/' }] }) } });
    const options = w.show.mock.calls[0][1];
    expect(options.actions).toEqual([{ action: 'first', title: 'First' }, { action: 'second', title: 'Second' }]);
    expect(options.data.actions).toEqual({ first: 'https://one.example/', second: 'https://two.example/' });
  });

  it('preserves Unicode button titles within the server character limit', async () => {
    const w = workerContext();
    const title = '📣'.repeat(40);
    await w.dispatch('push', { data: { json: () => ({ actions: [{ action: 'view', title }] }) } });
    expect(w.show.mock.calls[0][1].actions).toEqual([{ action: 'view', title }]);
  });

  it.each([0, 1, 2])('respects a browser maximum of %i action buttons while retaining the notification', async maxActions => {
    const w = workerContext(base, { maxActions });
    await w.dispatch('push', { data: { json: () => ({ title: 'Core notification', body: 'Still shown', actions: [{ action: 'first', title: 'First' }, { action: 'second', title: 'Second' }] }) } });
    expect(w.show).toHaveBeenCalledOnce();
    expect(w.show.mock.calls[0][0]).toBe('Core notification');
    expect(w.show.mock.calls[0][1].body).toBe('Still shown');
    expect(w.show.mock.calls[0][1].actions?.length ?? 0).toBe(maxActions);
  });

  it('retries unsupported rich options as a basic notification and acknowledges only the successful display', async () => {
    const w = workerContext();
    w.show.mockRejectedValueOnce(new TypeError('image/actions unsupported'));
    await w.dispatch('push', { data: { json: () => ({ title: 'Compatible', body: 'Basic fallback', image: 'https://cdn.example/banner.png', delivery_id: 'one', event_token: 'proof', actions: [{ action: 'view', title: 'View', url: 'https://landing.example/' }] }) } });
    expect(w.show).toHaveBeenCalledTimes(2);
    expect(w.show.mock.calls[1]).toEqual(['Compatible', expect.objectContaining({ body: 'Basic fallback', tag: 'd1-one', renotify: false, data: expect.objectContaining({ actions: {} }) })]);
    expect(w.show.mock.calls[1][1]).not.toHaveProperty('image');
    expect(w.show.mock.calls[1][1]).not.toHaveProperty('actions');
    expect(w.fetcher).toHaveBeenCalledOnce();
    expect(w.show.mock.invocationCallOrder[1]).toBeLessThan(w.fetcher.mock.invocationCallOrder[0]);
    expect(JSON.parse(w.fetcher.mock.calls[0][1].body).event).toBe('displayed');
  });

  it('does not retry successful rich notifications or non-compatibility failures', async () => {
    const w = workerContext();
    const payload = { image: 'https://cdn.example/banner.png', delivery_id: 'one', event_token: 'proof' };
    await w.dispatch('push', { data: { json: () => payload } });
    expect(w.show).toHaveBeenCalledOnce();
    expect(w.fetcher).toHaveBeenCalledOnce();
    w.show.mockClear().mockRejectedValueOnce(new Error('permission denied'));
    w.fetcher.mockClear();
    await expect(w.dispatch('push', { data: { json: () => payload } })).rejects.toThrow('permission denied');
    expect(w.show).toHaveBeenCalledOnce();
    expect(w.fetcher).not.toHaveBeenCalled();
  });

  it('does not report a display when both rich and basic notifications fail', async () => {
    const w = workerContext();
    w.show.mockRejectedValueOnce(new TypeError('unsupported image')).mockRejectedValueOnce(new Error('permission denied'));
    await expect(w.dispatch('push', { data: { json: () => ({ image: 'https://cdn.example/banner.png', delivery_id: 'one', event_token: 'proof' }) } })).rejects.toThrow('permission denied');
    expect(w.show).toHaveBeenCalledTimes(2);
    expect(w.fetcher).not.toHaveBeenCalled();
  });

  it('does not retry a TypeError from denied notification permission as a compatibility issue', async () => {
    const w = workerContext(base, { permission: 'denied' });
    w.show.mockRejectedValueOnce(new TypeError('permission denied'));
    await expect(w.dispatch('push', { data: { json: () => ({ image: 'https://cdn.example/banner.png', delivery_id: 'one', event_token: 'proof' }) } })).rejects.toThrow('permission denied');
    expect(w.show).toHaveBeenCalledOnce();
    expect(w.fetcher).not.toHaveBeenCalled();
  });
});

describe('PWA permission entry point', () => {
  it('registers only its own scope without requesting notification permission on page load', async () => {
    document.body.innerHTML = '<button id="install-button"></button><span id="install-status"></span><button id="notification-button"></button><button id="unsubscribe-button" hidden></button><span id="notification-status"></span><div id="install-guide" hidden><span id="install-guide-text"></span></div>';
    document.body.dataset.base = '/p/0123456789abcdef/';
    document.body.dataset.locale = 'en';
    document.body.dataset.pushEnabled = 'true';
    const requestPermission = vi.fn().mockResolvedValue('granted');
    const registration = { pushManager: { getSubscription: vi.fn().mockResolvedValue(null) } };
    const register = vi.fn().mockResolvedValue(registration);
    const fetcher = vi.fn().mockResolvedValue(new Response(JSON.stringify({ enabled: true, public_key: 'a' })));
    runInNewContext(client, {
      window: { isSecureContext: true, Notification: {}, PushManager: {}, matchMedia: () => ({ matches: false }), addEventListener: vi.fn() },
      navigator: { userAgent: 'Chrome', platform: 'Linux', maxTouchPoints: 0, serviceWorker: { register, ready: Promise.resolve(registration) } },
      Notification: { permission: 'default', requestPermission },
      document, localStorage, fetch: fetcher, Uint8Array, atob,
    });
    await vi.waitFor(() => expect(fetcher).toHaveBeenCalledOnce());
    expect(register).toHaveBeenCalledWith('/p/0123456789abcdef/sw.js', { scope: '/p/0123456789abcdef/', updateViaCache: 'none' });
    expect(requestPermission).not.toHaveBeenCalled();
  });
});
