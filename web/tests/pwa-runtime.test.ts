import { readFileSync } from 'node:fs';
import { runInNewContext } from 'node:vm';
import { resolve } from 'node:path';
import { describe, expect, it, vi } from 'vitest';

const worker = readFileSync(resolve(process.cwd(), '../internal/platform/templates/pwa-sw.js'), 'utf8');
const client = readFileSync(resolve(process.cwd(), '../internal/platform/templates/pwa-client.js'), 'utf8');
const base = 'https://d1.example/p/0123456789abcdef/';

function workerContext(cacheStore?: Map<string, Response>) {
  const listeners = new Map<string, (event: any) => void>();
  const fetcher = vi.fn().mockResolvedValue(new Response('{}'));
  const show = vi.fn().mockResolvedValue(undefined);
  const open = vi.fn().mockResolvedValue(undefined);
  const focus = vi.fn().mockResolvedValue(undefined);
  const self = {
    location: { href: base + 'sw.js' },
    addEventListener: (name: string, handler: (event: any) => void) => listeners.set(name, handler),
    registration: { showNotification: show },
    clients: { claim: vi.fn(), matchAll: vi.fn().mockResolvedValue([{ url: base + 'launch', focus }]), openWindow: open },
    skipWaiting: vi.fn(),
  };
  const caches = cacheStore ? { open: async () => ({
    match: async (key: string) => cacheStore.get(String(key))?.clone(),
    put: async (key: string, value: Response) => { cacheStore.set(String(key), value); },
    keys: async () => [...cacheStore.keys()],
    delete: async (key: string) => cacheStore.delete(String(key)),
  }) } : undefined;
  runInNewContext(worker, { self, URL, Response, fetch: fetcher, caches });
  const dispatch = async (name: string, values: Record<string, any>) => {
    let pending: Promise<any> | undefined;
    listeners.get(name)!({ ...values, waitUntil: (value: Promise<any>) => { pending = value; } });
    await pending;
  };
  return { listeners, fetcher, show, open, focus, dispatch };
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
    expect(w.show.mock.invocationCallOrder[0]).toBeLessThan(w.fetcher.mock.invocationCallOrder[0]);
    const [url, request] = w.fetcher.mock.calls[0];
    expect(String(url)).toBe(base + 'event');
    expect(JSON.parse(request.body)).toEqual({ delivery_id: 'delivery-one', token: 'event-capability', event: 'displayed' });
    expect(request.credentials).toBe('omit');
  });

  it('focuses the installed app and reports clicks using its delivery capability', async () => {
    const w = workerContext();
    const close = vi.fn();
    await w.dispatch('notificationclick', { notification: { close, data: { url: 'https://d1.example/#/admin/users', delivery_id: 'one', event_token: 'secret' } } });
    expect(close).toHaveBeenCalledOnce();
    expect(w.focus).toHaveBeenCalledOnce();
    expect(w.open).not.toHaveBeenCalled();
    expect(JSON.parse(w.fetcher.mock.calls[0][1].body).event).toBe('clicked');
  });

  it('does not claim a display when the operating system rejects the notification', async () => {
    const w = workerContext();
    w.show.mockRejectedValueOnce(new Error('permission denied'));
    await expect(w.dispatch('push', { data: { json: () => ({ title: 'A', delivery_id: 'one', event_token: 'secret' }) } })).rejects.toThrow('permission denied');
    expect(w.fetcher).not.toHaveBeenCalled();
  });

  it('suppresses a repeated delivery even after the service worker restarts', async () => {
    const receipts = new Map<string, Response>();
    const first = workerContext(receipts);
    const payload = { title: 'Only once', delivery_id: 'a'.repeat(32), event_token: 'proof' };
    await first.dispatch('push', { data: { json: () => payload } });
    expect(first.show).toHaveBeenCalledOnce();
    const restarted = workerContext(receipts);
    await restarted.dispatch('push', { data: { json: () => payload } });
    expect(restarted.show).not.toHaveBeenCalled();
    expect(JSON.parse(restarted.fetcher.mock.calls[0][1].body).event).toBe('displayed');
    expect([...receipts.keys()]).toEqual([base + 'notification-receipt/' + payload.delivery_id]);
    expect(await receipts.values().next().value!.clone().text()).toMatch(/^\d+$/);
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
