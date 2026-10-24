import { StrictMode } from 'react';
import { act, cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { authProvider } from '../src/api';
import { Root } from '../src/root';
import type { Config, Dashboard, Plan, User } from '../src/types';

const plan: Plan = { id: 'free', name: '免费版', price_micros: 0, app_limit: 20, download_limit: 2000, storage_bytes: 2 ** 30 };
const config: Config = {
  currency: 'USD', site_name: 'D1 Apk', development: true, registration_open: true,
  recharge_bonus_enabled: true, recharge_bonus_tiers: [{ threshold_micros: 200000000, percent: 15 }, { threshold_micros: 500000000, percent: 35 }, { threshold_micros: 1000000000, percent: 50 }],
  turnstile_site_key: '', public_url: 'http://localhost:3000', download_base_url: '', download_domain_mode: 'unified',
  cname_target: 'dns.d1apk.com', plans: [plan], download_price_micros: 2000,
  max_upload_bytes: 2 ** 30, domain_limit: 5, payment_methods: [], support_email: '', usdt_rate_micros: 1000000,
};
const account: User = {
  id: 'test-member', email: 'member@example.com', name: '测试用户', role: 'member',
  disabled: false, balance_micros: 0, held_micros: 0, pro_until: null, created_at: '2026-10-01T00:00:00Z',
};
const dashboard: Dashboard = {
  user: account, plan, usage: { period: '2026-10', free_used: 0, free_reserved: 0, paid_used: 0, spent_micros: 0 },
  quota_limit: 1000, quota_used: 0, quota_reserved: 0, quota_remaining: 1000, quota_renews_at: null, app_count: 0, storage_bytes: 0, total_downloads: 0, active_domains: 0, quota_resets_at: '2026-11-01T00:00:00Z',
  series: [{ date: '10-01', count: 0, paid: 0 }, { date: '10-02', count: 0, paid: 0 }],
};
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });

let requests: string[];
let signedIn: boolean;
let identityGate: Promise<void>;
let registrationSubmissions: Record<string, unknown>[];
beforeEach(() => {
  requests = [];
  signedIn = false;
  identityGate = Promise.resolve();
  registrationSubmissions = [];
  const storage = new Map<string, string>();
  vi.stubGlobal('localStorage', {
    getItem: (key: string) => storage.get(key) ?? null,
    setItem: (key: string, value: string) => { storage.set(key, String(value)); },
    removeItem: (key: string) => { storage.delete(key); },
    clear: () => storage.clear(),
    key: (index: number) => [...storage.keys()][index] ?? null,
    get length() { return storage.size; },
  });
  vi.stubGlobal('scrollTo', vi.fn());
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const path = String(input).split('?')[0];
    requests.push(path);
    switch (path) {
      case '/api/config': return json(config);
      case '/api/auth/captcha': return json({ id: 'test-challenge', image: 'data:image/svg+xml,<svg xmlns="http://www.w3.org/2000/svg"/>' });
      case '/api/me': {
        // Capture the server response when the request starts. An anonymous
        // response can arrive after registration has established a session.
        const response = signedIn ? json({ user: account, plan, csrf_token: 'test-csrf' }) : json({ error: '请先登录' }, 401);
        if (!signedIn) await identityGate;
        return response;
      }
      case '/api/auth/register':
        registrationSubmissions.push(JSON.parse(String(options?.body)));
        signedIn = true;
        return json({ user: account, csrf_token: 'test-csrf' });
      case '/api/auth/login': signedIn = true; return json({ user: account, csrf_token: 'test-csrf' });
      case '/api/auth/logout': signedIn = false; return json({ ok: true });
      case '/api/download-domain': return json({ mode: 'unified', plan_id: 'free', download_base_url: '' });
      case '/api/dashboard': return json(dashboard);
      case '/api/apps': return json({ data: [], total: 0 });
      default: throw new Error(`Unexpected request: ${input}`);
    }
  }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function open(path: string) {
  window.history.replaceState(null, '', `/#${path}`);
  return render(<StrictMode><Root /></StrictMode>);
}

describe('anonymous visitors and permissions', () => {
  it('returns no role for anonymous visitors while still rejecting authentication', async () => {
    await expect(authProvider.getPermissions!({})).resolves.toBeNull();
    await expect(authProvider.checkAuth({})).rejects.toMatchObject({ status: 401 });
    await expect(authProvider.checkError({ status: 401 })).rejects.toMatchObject({ status: 401 });
  });

  it.each([403, 500])('does not hide a permissions API failure (%i)', async status => {
    vi.mocked(fetch).mockResolvedValueOnce(json({ error: '权限查询失败' }, status));
    await expect(authProvider.getPermissions!({})).rejects.toMatchObject({ status });
  });

  it.each(['member', 'admin'] as const)('keeps the authenticated %s role', async role => {
    vi.mocked(fetch).mockResolvedValueOnce(json({ user: { ...account, role } }));
    await expect(authProvider.getPermissions!({})).resolves.toBe(role);
  });

  it('ignores a cancelled old identity response before it can replace the new session CSRF token', async () => {
    let finishOldIdentity!: () => void;
    const oldIdentityGate = new Promise<void>(resolve => { finishOldIdentity = resolve; });
    vi.mocked(fetch).mockImplementationOnce(async () => {
      await oldIdentityGate;
      return json({ user: { ...account, role: 'admin' }, plan, csrf_token: 'old-session-csrf' });
    });
    const controller = new AbortController();
    const oldIdentity = authProvider.getIdentity!({ signal: controller.signal });
    controller.abort();
    await authProvider.login({ action: 'register', email: account.email });
    finishOldIdentity();
    await expect(oldIdentity).rejects.toMatchObject({ name: 'AbortError' });
    await authProvider.logout({});
    const logoutCall = vi.mocked(fetch).mock.calls.find(([path]) => path === '/api/auth/logout');
    expect(new Headers(logoutCall?.[1]?.headers).get('X-CSRF-Token')).toBe('test-csrf');
  });
});

describe('real application routes', () => {
  async function submitRegistration(user: ReturnType<typeof userEvent.setup>) {
    await user.type(screen.getByRole('textbox', { name: '用户名称', exact: true }), '受邀好友');
    await user.type(screen.getByRole('textbox', { name: '邮箱地址', exact: true }), account.email);
    await user.type(screen.getByLabelText(/^密码/), 'test-password-123');
    await user.type(screen.getByRole('textbox', { name: /图形验证码/ }), '12345');
    await user.click(screen.getByRole('button', { name: '免费创建账户' }));
  }

  it('registers an invited user directly into the console, survives a remount, and supports logout and login', async () => {
    const user = userEvent.setup();
    const inviteCode = '0123456789abcdef';
    const view = open(`/register?invite=${inviteCode}`);
    await screen.findByRole('heading', { name: '创建你的账户' });
    await waitFor(() => expect(requests).toContain('/api/me'));
    await submitRegistration(user);
    await screen.findByRole('heading', { name: '工作台' });
    expect(window.location.hash).toBe('#/');
    expect(signedIn).toBe(true);
    expect(registrationSubmissions).toEqual([{ email: account.email, name: '受邀好友', password: 'test-password-123', captcha_id: 'test-challenge', captcha_code: '12345', invite_code: inviteCode }]);
    expect(requests.filter(path => path === '/api/auth/register')).toHaveLength(1);
    expect(requests).not.toContain('/api/auth/login');
    expect(requests).not.toContain('/api/auth/logout');

    // A fresh app instance rechecks the established server session, as on reload.
    view.unmount();
    render(<StrictMode><Root /></StrictMode>);
    await screen.findByRole('heading', { name: '工作台' });
    await expect(authProvider.checkAuth({})).resolves.toBeUndefined();
    expect(requests).not.toContain('/api/auth/logout');

    await user.click(screen.getByTitle('退出登录'));
    await screen.findByRole('heading', { name: '登录控制台' });
    expect(signedIn).toBe(false);
    await user.type(screen.getByRole('textbox', { name: '账号', exact: true }), account.email);
    await user.type(screen.getByLabelText('密码', { exact: true }), 'test-password-123');
    await user.type(screen.getByRole('textbox', { name: /图形验证码/ }), '12345');
    await user.click(screen.getByRole('button', { name: /登 录/ }));
    await screen.findByRole('heading', { name: '工作台' });
    expect(signedIn).toBe(true);
    expect(requests.filter(path => path === '/api/auth/logout')).toHaveLength(1);
    expect(requests.filter(path => path === '/api/auth/login')).toHaveLength(1);
  });

  it('keeps the registered session when an earlier anonymous check finishes with a delayed 401', async () => {
    let finishAnonymousChecks!: () => void;
    identityGate = new Promise<void>(resolve => { finishAnonymousChecks = resolve; });
    const user = userEvent.setup();
    open('/register?invite=0123456789abcdef');
    await screen.findByRole('heading', { name: '创建你的账户' });
    await waitFor(() => expect(requests).toContain('/api/me'));
    await submitRegistration(user);
    await screen.findByRole('heading', { name: '工作台' });
    await act(async () => { finishAnonymousChecks(); });
    await waitFor(() => expect(screen.getByRole('heading', { name: '工作台' })).toBeTruthy());
    expect(window.location.hash).toBe('#/');
    expect(signedIn).toBe(true);
    expect(requests).not.toContain('/api/auth/login');
    expect(requests).not.toContain('/api/auth/logout');
    expect(screen.queryByText(/平台管理/)).toBeNull();
    await expect(authProvider.checkAuth({})).resolves.toBeUndefined();
  });

  it('preserves a half-typed registration email after delayed anonymous checks and their retry window', async () => {
    let finishChecks!: () => void;
    identityGate = new Promise<void>(resolve => { finishChecks = resolve; });
    const user = userEvent.setup();
    open('/register');
    await screen.findByRole('heading', { name: '创建你的账户' });
    await user.type(screen.getByRole('textbox', { name: '用户名称', exact: true }), '注册测试');
    await user.type(screen.getByRole('textbox', { name: '邮箱地址', exact: true }), 'half.typed');
    expect(requests).toContain('/api/me');

    await act(async () => { finishChecks(); });
    // React Query retries failed permissions at 1, 2, and 4 seconds. The old
    // provider redirected only after these retries, while the visitor typed.
    await act(async () => { await new Promise(resolve => setTimeout(resolve, 8200)); });

    expect(window.location.hash).toBe('#/register');
    expect((screen.getByRole('textbox', { name: '邮箱地址', exact: true }) as HTMLInputElement).value).toBe('half.typed');
    expect((screen.getByRole('textbox', { name: '用户名称', exact: true }) as HTMLInputElement).value).toBe('注册测试');
    expect(requests).not.toContain('/api/auth/logout');
    expect(requests).not.toContain('/api/auth/register');
    await user.type(screen.getByRole('textbox', { name: '邮箱地址', exact: true }), '@example.com');
    expect((screen.getByRole('textbox', { name: '邮箱地址', exact: true }) as HTMLInputElement).value).toBe('half.typed@example.com');
  }, 15000);

  it.each(['/apps', '/referrals', '/admin/settings'])('redirects an anonymous visitor away from protected %s', async path => {
    open(path);
    await screen.findByRole('heading', { name: '登录控制台' });
    expect(window.location.hash).toBe('#/login');
    expect(screen.queryByText('我的工作空间')).toBeNull();
    expect(requests).not.toContain('/api/apps');
    expect(requests).not.toContain('/api/admin/settings');
  });

  it('can navigate from the public welcome page through registration and login into the console', async () => {
    const user = userEvent.setup();
    open('/welcome');
    await user.click(await screen.findByRole('link', { name: /开始免费分发/ }));
    await screen.findByRole('heading', { name: '创建你的账户' });
    await user.click(screen.getByRole('link', { name: /立即登录/ }));
    await screen.findByRole('heading', { name: '登录控制台' });
    await user.type(screen.getByRole('textbox', { name: '账号', exact: true }), account.email);
    await user.type(screen.getByLabelText('密码', { exact: true }), 'test-password-123');
    await user.type(screen.getByRole('textbox', { name: /图形验证码/ }), '12345');
    await user.click(screen.getByRole('button', { name: /登 录/ }));
    await screen.findByRole('heading', { name: '工作台' });
    await waitFor(() => expect(window.location.hash).toBe('#/'));
    expect(screen.queryByText(/平台管理/)).toBeNull();
    expect(requests).toContain('/api/auth/login');
    expect(requests).not.toContain('/api/auth/logout');
  });
});
