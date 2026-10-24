import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { resolve } from 'node:path';
import { describe, expect, it, vi } from 'vitest';

const worker = readFileSync(resolve(process.cwd(), '../internal/platform/templates/pwa-sw.js'), 'utf8');
const client = readFileSync(resolve(process.cwd(), '../internal/platform/templates/pwa-client.js'), 'utf8');
const base = 'https://d1.example/p/0123456789abcdef/';

function workerContext(workerBase = base) {
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
  runInNewContext(worker, { self, URL, Response, fetch: fetcher });
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
    const payload = { title: 'Hello', body: 'A new message', icon: 'https://untrusted.example/icon', url: 'javascript:alert(1)', delivery_id: 'delivery-one', event_token: 'event-capability' };
    await w.dispatch('push', { data: { json: () => payload } });
    await w.dispatch('push', { data: { json: () => payload } });
    const first = w.show.mock.calls[0];
    expect(first[0]).toBe('Hello');
    expect(first[1].icon).toBe(base + 'icon/192.png');
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
