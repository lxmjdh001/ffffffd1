import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { runInNewContext } from 'node:vm';
import { beforeEach, describe, expect, it, vi } from 'vitest';

const client = readFileSync(resolve(process.cwd(), '../internal/platform/templates/pwa-client.js'), 'utf8');
const base = '/p/0123456789abcdef/';
const storeKey = 'd1-pwa-subscription:' + base;
const capability = { subscription_id: 'subscription-one', token: 'subscription-one.proof' };
const configuration = { enabled: true, public_key: 'AQID', subscribe_url: base + 'subscribe', unsubscribe_url: base + 'unsubscribe' };

function subscription(endpoint = 'https://web.push.apple.com/new-endpoint') {
  return {
    toJSON: () => ({ endpoint, keys: { p256dh: 'browser-public-key', auth: 'browser-auth-key' } }),
    unsubscribe: vi.fn().mockResolvedValue(true),
  };
}

type Options = {
  permission?: string;
  existing?: ReturnType<typeof subscription> | null;
  saved?: typeof capability & { unsubscribed?: boolean };
  config?: typeof configuration;
  ios?: boolean;
  standalone?: boolean;
  subscribeFailures?: number;
  subscribeConflict?: boolean;
  unsubscribeFailures?: number;
  configFailure?: boolean;
  holdSubscribe?: Promise<void>;
};

function start(options: Options = {}) {
  document.body.innerHTML = '<button id="install-button"></button><span id="install-status"></span><button id="notification-button">Enable notifications</button><button id="unsubscribe-button" hidden></button><span id="notification-status"></span><div id="install-guide" hidden><span id="install-guide-text"></span></div>';
  document.body.dataset.base = base;
  document.body.dataset.locale = 'en';
  document.body.dataset.pushEnabled = 'true';
  if (options.saved) localStorage.setItem(storeKey, JSON.stringify(options.saved));
  let current = options.existing || null;
  let subscribeFailures = options.subscribeFailures || 0;
  let unsubscribeFailures = options.unsubscribeFailures || 0;
  const notification = {
    permission: options.permission || 'default',
    requestPermission: vi.fn(async () => { notification.permission = 'granted'; return 'granted'; }),
  };
  const subscribe = vi.fn(async () => {
    current = subscription('https://fcm.googleapis.com/fcm/send/recreated');
    current.unsubscribe.mockImplementation(async () => { current = null; return true; });
    return current;
  });
  if (current) current.unsubscribe.mockImplementation(async () => { current = null; return true; });
  const registration = { pushManager: { getSubscription: vi.fn(async () => current), subscribe } };
  const register = vi.fn().mockResolvedValue(registration);
  const fetcher = vi.fn(async (url: string, request?: RequestInit) => {
    if (url.endsWith('push-config')) return new Response(JSON.stringify(options.config || configuration), { status: options.configFailure ? 503 : 200 });
    if (url.endsWith('/subscribe')) {
      if (options.holdSubscribe) await options.holdSubscribe;
      if (options.subscribeConflict && JSON.parse(request!.body as string).automatic) return new Response(JSON.stringify({ error: '订阅已关闭', code: 'subscription_unsubscribed' }), { status: 409 });
      if (subscribeFailures-- > 0) return new Response(JSON.stringify({ error: 'Temporary outage' }), { status: 503 });
      return new Response(JSON.stringify({ ...capability, status: 'active' }));
    }
    if (url.endsWith('/unsubscribe') && unsubscribeFailures-- > 0) return new Response(JSON.stringify({ error: 'Temporary outage' }), { status: 503 });
    return new Response('{}');
  });
  runInNewContext(client, {
    window: { isSecureContext: true, Notification: {}, PushManager: {}, matchMedia: () => ({ matches: options.standalone || false }), addEventListener: vi.fn() },
    navigator: { userAgent: options.ios ? 'iPhone Safari' : 'Android Chrome', platform: options.ios ? 'iPhone' : 'Linux', maxTouchPoints: 1, serviceWorker: { register, ready: Promise.resolve(registration) } },
    Notification: notification, document, localStorage, fetch: fetcher, Uint8Array, atob,
  });
  const enable = document.getElementById('notification-button') as HTMLButtonElement;
  const turnOff = document.getElementById('unsubscribe-button') as HTMLButtonElement;
  const status = () => document.getElementById('notification-status')!.textContent;
  const calls = (suffix = '/subscribe') => fetcher.mock.calls.filter(([url]) => url.endsWith(suffix));
  const saved = () => JSON.parse(localStorage.getItem(storeKey) || 'null');
  return { notification, registration, register, subscribe, fetcher, enable, turnOff, status, calls, saved };
}

beforeEach(() => localStorage.clear());

describe('PWA subscription renewal follows the reference PWA', () => {
  it('does not request permission on load and requests it directly on an explicit click', async () => {
    const c = start();
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
    expect(c.calls()).toHaveLength(0);
    c.enable.click();
    expect(c.notification.requestPermission).toHaveBeenCalledOnce();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(c.calls()).toHaveLength(1);
    expect(c.subscribe).toHaveBeenCalledWith({ userVisibleOnly: true, applicationServerKey: new Uint8Array([1, 2, 3]) });
    expect(c.notification.requestPermission.mock.invocationCallOrder[0]).toBeLessThan(c.subscribe.mock.invocationCallOrder[0]);
  });

  it('automatically syncs the browser endpoint with the original renewal capability', async () => {
    const existing = subscription();
    const c = start({ permission: 'granted', existing, saved: capability });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(c.calls()).toHaveLength(1);
    expect(JSON.parse(c.calls()[0][1]!.body as string)).toEqual({ ...existing.toJSON(), token: capability.token, automatic: true });
    expect(c.saved()).toEqual(capability);
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
    expect(c.subscribe).not.toHaveBeenCalled();
    expect(c.turnOff.hidden).toBe(false);
  });

  it('recovers browser-held subscriptions when local storage has been lost', async () => {
    const existing = subscription();
    const c = start({ permission: 'granted', existing });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(JSON.parse(c.calls()[0][1]!.body as string)).toEqual({ ...existing.toJSON(), automatic: true });
    expect(c.saved()).toEqual(capability);
  });

  it('recreates a missing browser subscription while retaining the server identity', async () => {
    const c = start({ permission: 'granted', saved: capability });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(c.subscribe).toHaveBeenCalledOnce();
    expect(JSON.parse(c.calls()[0][1]!.body as string).token).toBe(capability.token);
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
  });

  it('does not undo a legacy opt-out that removed browser and saved subscriptions', async () => {
    const c = start({ permission: 'granted' });
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    expect(c.subscribe).not.toHaveBeenCalled();
    expect(c.calls()).toHaveLength(0);
    expect(c.enable.textContent).toBe('Enable notifications');
    expect(c.turnOff.hidden).toBe(true);
    c.enable.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(c.subscribe).toHaveBeenCalledOnce();
    expect(c.calls()).toHaveLength(1);
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
  });

  it('keeps failed automatic sync visibly inactive and allows a manual retry', async () => {
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, subscribeFailures: 1 });
    await vi.waitFor(() => expect(c.status()).toContain('Unable to sync'));
    expect(c.enable.textContent).toBe('Enable notifications');
    expect(c.enable.disabled).toBe(false);
    expect(c.turnOff.hidden).toBe(false);
    c.enable.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(c.calls()).toHaveLength(2);
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
  });

  it('rolls back a newly created browser subscription when the server rejects it', async () => {
    const c = start({ permission: 'granted', saved: capability, subscribeFailures: 1 });
    await vi.waitFor(() => expect(c.status()).toContain('Unable to sync'));
    const failed = await c.subscribe.mock.results[0].value;
    expect(failed.unsubscribe).toHaveBeenCalledOnce();
    expect(c.saved()).toEqual(capability);
    c.enable.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(c.subscribe).toHaveBeenCalledTimes(2);
  });

  it('does not race a manual click with an initialization sync already in flight', async () => {
    let release!: () => void;
    const holdSubscribe = new Promise<void>(resolve => { release = resolve; });
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, holdSubscribe });
    await vi.waitFor(() => expect(c.calls()).toHaveLength(1));
    c.enable.click();
    c.turnOff.click();
    expect(c.calls()).toHaveLength(1);
    expect(c.calls('/unsubscribe')).toHaveLength(0);
    release();
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    expect(c.status()).toContain('Notifications enabled');
  });

  it('preserves a server opt-out conflict instead of automatically resubscribing', async () => {
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, subscribeConflict: true });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications turned off'));
    expect(c.calls()).toHaveLength(1);
    expect(c.saved()).toEqual({ ...capability, unsubscribed: true });
    expect(c.enable.textContent).toBe('Enable notifications');
    expect(c.turnOff.hidden).toBe(true);
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
    c.enable.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(JSON.parse(c.calls()[1][1]!.body as string)).toEqual({ ...subscription().toJSON(), token: capability.token });
    expect(c.saved()).toEqual(capability);
  });

  it('remembers a server opt-out even when browser storage had lost the capability', async () => {
    const c = start({ permission: 'granted', existing: subscription(), subscribeConflict: true });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications turned off'));
    expect(c.saved()).toEqual({ unsubscribed: true });
    const revisit = start({ permission: 'granted', existing: subscription() });
    await vi.waitFor(() => expect(revisit.enable.disabled).toBe(false));
    expect(revisit.calls()).toHaveLength(0);
    revisit.enable.click();
    await vi.waitFor(() => expect(revisit.status()).toContain('Notifications enabled'));
    expect(JSON.parse(revisit.calls()[0][1]!.body as string)).toEqual(subscription().toJSON());
  });

  it('does not let a delayed automatic success overwrite another window opt-out', async () => {
    let release!: () => void;
    const holdSubscribe = new Promise<void>(resolve => { release = resolve; });
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, holdSubscribe });
    await vi.waitFor(() => expect(c.calls()).toHaveLength(1));
    localStorage.setItem(storeKey, JSON.stringify({ ...capability, unsubscribed: true }));
    release();
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    expect(c.status()).toContain('Notifications turned off');
    expect(c.saved()).toEqual({ ...capability, unsubscribed: true });
    expect(c.calls('/unsubscribe')).toHaveLength(1);
    expect(JSON.parse(c.calls('/unsubscribe')[0][1]!.body as string)).toEqual(capability);
    expect(c.turnOff.hidden).toBe(true);
  });

  it('cleans up a just-created browser subscription when another window opts out during sync', async () => {
    let release!: () => void;
    const holdSubscribe = new Promise<void>(resolve => { release = resolve; });
    const c = start({ permission: 'granted', saved: capability, holdSubscribe });
    await vi.waitFor(() => expect(c.calls()).toHaveLength(1));
    const created = await c.subscribe.mock.results[0].value;
    localStorage.setItem(storeKey, JSON.stringify({ ...capability, unsubscribed: true }));
    release();
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    expect(created.unsubscribe).toHaveBeenCalledOnce();
    expect(c.saved()).toEqual({ ...capability, unsubscribed: true });
    expect(c.calls('/unsubscribe')).toHaveLength(1);
    expect(c.status()).toContain('Notifications turned off');
  });

  it('keeps a concurrent opt-out when the older automatic request fails', async () => {
    let release!: () => void;
    const holdSubscribe = new Promise<void>(resolve => { release = resolve; });
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, holdSubscribe, subscribeFailures: 1 });
    await vi.waitFor(() => expect(c.calls()).toHaveLength(1));
    localStorage.setItem(storeKey, JSON.stringify({ ...capability, unsubscribed: true }));
    release();
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    expect(c.status()).toContain('Notifications turned off');
    expect(c.saved()).toEqual({ ...capability, unsubscribed: true });
    expect(c.turnOff.hidden).toBe(true);
  });

  it('does not automatically subscribe an iPhone browser outside the installed app', async () => {
    const c = start({ permission: 'granted', ios: true, existing: subscription(), saved: capability });
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    expect(c.calls()).toHaveLength(0);
    expect(c.subscribe).not.toHaveBeenCalled();
    c.enable.click();
    await vi.waitFor(() => expect(document.getElementById('install-guide')!.hidden).toBe(false));
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
  });

  it('automatically syncs an already authorized installed iPhone app', async () => {
    const c = start({ permission: 'granted', ios: true, standalone: true, existing: subscription(), saved: capability });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    expect(c.calls()).toHaveLength(1);
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
  });

  it('does not subscribe while app notifications are disabled and keeps unsubscribe available', async () => {
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, config: { ...configuration, enabled: false } });
    await vi.waitFor(() => expect(c.status()).toContain('currently unavailable'));
    expect(c.enable.hidden).toBe(true);
    expect(c.calls()).toHaveLength(0);
    expect(c.subscribe).not.toHaveBeenCalled();
    expect(c.turnOff.hidden).toBe(false);
    c.turnOff.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications turned off'));
  });

  it('preserves an explicit opt-out across visits and renews with the same identity only on click', async () => {
    const c = start({ permission: 'granted', existing: subscription(), saved: capability });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    c.turnOff.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications turned off'));
    expect(c.saved()).toEqual({ ...capability, unsubscribed: true });
    expect(JSON.parse(c.calls('/unsubscribe')[0][1]!.body as string)).toEqual(capability);
    const revisit = start({ permission: 'granted' });
    await vi.waitFor(() => expect(revisit.enable.disabled).toBe(false));
    expect(revisit.calls()).toHaveLength(0);
    expect(revisit.subscribe).not.toHaveBeenCalled();
    expect(revisit.turnOff.hidden).toBe(true);
    revisit.enable.click();
    await vi.waitFor(() => expect(revisit.status()).toContain('Notifications enabled'));
    expect(JSON.parse(revisit.calls()[0][1]!.body as string).token).toBe(capability.token);
    expect(revisit.saved()).toEqual(capability);
  });

  it('keeps a subscription active if the server fails to process unsubscribe', async () => {
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, unsubscribeFailures: 1 });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    c.turnOff.click();
    await vi.waitFor(() => expect(c.status()).toBe('Temporary outage'));
    expect(c.saved()).toEqual(capability);
    expect(c.turnOff.hidden).toBe(false);
    c.turnOff.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications turned off'));
  });

  it('preserves successful server opt-out even if browser cleanup fails', async () => {
    const existing = subscription();
    const c = start({ permission: 'granted', existing, saved: capability });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications enabled'));
    existing.unsubscribe.mockRejectedValueOnce(new Error('Browser cleanup failed'));
    c.turnOff.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications turned off'));
    expect(c.saved()).toEqual({ ...capability, unsubscribed: true });
    const revisit = start({ permission: 'granted', existing });
    await vi.waitFor(() => expect(revisit.enable.disabled).toBe(false));
    expect(revisit.calls()).toHaveLength(0);
  });

  it('does not subscribe when the user dismisses the permission prompt', async () => {
    const c = start();
    await vi.waitFor(() => expect(c.enable.disabled).toBe(false));
    c.notification.requestPermission.mockResolvedValueOnce('default');
    c.enable.click();
    await vi.waitFor(() => expect(c.status()).toContain('Notifications were not enabled'));
    expect(c.subscribe).not.toHaveBeenCalled();
    expect(c.calls()).toHaveLength(0);
    expect(c.enable.disabled).toBe(false);
  });

  it('does not claim subscription success when configuration cannot be loaded', async () => {
    const c = start({ permission: 'granted', existing: subscription(), saved: capability, configFailure: true });
    await vi.waitFor(() => expect(c.status()).toContain('Unable to prepare'));
    expect(c.enable.textContent).toBe('Enable notifications');
    expect(c.enable.disabled).toBe(true);
    expect(c.calls()).toHaveLength(0);
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
  });

  it('does not sync or request permission after browser permission has been denied', async () => {
    const c = start({ permission: 'denied', saved: capability });
    await vi.waitFor(() => expect(c.status()).toContain('Notifications are blocked'));
    expect(c.calls()).toHaveLength(0);
    c.enable.click();
    expect(c.notification.requestPermission).not.toHaveBeenCalled();
    expect(c.subscribe).not.toHaveBeenCalled();
  });
});
