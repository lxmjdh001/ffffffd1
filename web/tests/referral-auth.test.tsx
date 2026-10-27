import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthPage } from '../src/auth';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

vi.mock('react-admin', () => ({ useNotificationContext: () => ({ resetNotifications: vi.fn() }) }));
const inviteCode = '0123456789abcdef';
const config = { site_name: 'D1 Apk', development: true, registration_open: true } as Config;
let submissions: Record<string, unknown>[];
let client: QueryClient;
let registrationSucceeds: boolean;

beforeEach(() => {
  window.history.replaceState(null, '', '/');
  submissions = [];
  registrationSucceeds = false;
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    if (String(input) === '/api/auth/captcha') return new Response(JSON.stringify({ id: 'challenge-1', image: 'data:image/svg+xml,<svg/>' }));
    if (String(input) === '/api/auth/register') {
      submissions.push(JSON.parse(String(options?.body)));
      if (registrationSucceeds) return new Response(JSON.stringify({ user: { id: 'new-member', name: '受邀好友', role: 'member' }, csrf_token: 'new-session-csrf' }));
      return new Response(JSON.stringify({ error: '邀请码无效，请检查后重试' }), { status: 400 });
    }
    throw new Error(`Unexpected request: ${input}`);
  }));
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function open(path: string) {
  render(<QueryClientProvider client={client}><ConfigContext.Provider value={{ config, reload: vi.fn() }}><MemoryRouter initialEntries={[path]}><Routes><Route path="/register" element={<AuthPage register />} /><Route path="/login" element={<AuthPage />} /><Route path="/" element={<h1>工作台</h1>} /></Routes></MemoryRouter></ConfigContext.Provider></QueryClientProvider>);
  return userEvent.setup();
}

async function fillAndSubmit(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByRole('textbox', { name: '用户名称', exact: true }), '受邀好友');
  await user.type(screen.getByRole('textbox', { name: '邮箱地址', exact: true }), 'invited@example.com');
  await user.type(screen.getByLabelText(/^密码/), 'test-password-123');
  await user.type(screen.getByRole('textbox', { name: /图形验证码/ }), '12345');
  await user.click(screen.getByRole('button', { name: '创建账户' }));
}

describe('registration invitations', () => {
  it('shows the link invite and preserves an edited code through login and registration', async () => {
    const user = open(`/register?invite=${inviteCode}`);
    const input = screen.getByRole('textbox', { name: '邀请码（选填）' }) as HTMLInputElement;
    expect(input.value).toBe(inviteCode);
    await user.clear(input); await user.type(input, 'fedcba9876543210');
    expect(screen.getByRole('link', { name: /立即登录/ }).getAttribute('href')).toBe('/login?invite=fedcba9876543210');
    await user.click(screen.getByRole('link', { name: /立即登录/ }));
    expect(screen.getByRole('heading', { name: '登录控制台' })).toBeTruthy();
    await user.click(screen.getByRole('link', { name: /立即注册/ }));
    expect((screen.getByRole('textbox', { name: '邀请码（选填）' }) as HTMLInputElement).value).toBe('fedcba9876543210');
  });

  it('accepts a normal URL query and lets the hash-route invitation take precedence', () => {
    window.history.replaceState(null, '', `/?invite=${inviteCode}#/register`);
    open('/register');
    expect((screen.getByRole('textbox', { name: '邀请码（选填）' }) as HTMLInputElement).value).toBe(inviteCode);
    cleanup();
    open('/register?invite=fedcba9876543210');
    expect((screen.getByRole('textbox', { name: '邀请码（选填）' }) as HTMLInputElement).value).toBe('fedcba9876543210');
  });

  it('submits the trimmed invite and keeps the form editable after an invalid-code response', async () => {
    const user = open(`/register?invite=${inviteCode}`);
    const input = screen.getByRole('textbox', { name: '邀请码（选填）' });
    await user.clear(input); await user.type(input, `  ${inviteCode}  `);
    await fillAndSubmit(user);
    await waitFor(() => expect(submissions).toEqual([{ email: 'invited@example.com', name: '受邀好友', password: 'test-password-123', captcha_id: 'challenge-1', captcha_code: '12345', invite_code: inviteCode }]));
    expect((await screen.findByRole('alert')).textContent).toContain('邀请码无效');
    expect((input as HTMLInputElement).value).toBe(`  ${inviteCode}  `);
    expect((screen.getByRole('textbox', { name: '邮箱地址', exact: true }) as HTMLInputElement).value).toBe('invited@example.com');
    await waitFor(() => expect((screen.getByRole('textbox', { name: /图形验证码/ }) as HTMLInputElement).value).toBe(''));
    expect(vi.mocked(fetch).mock.calls.filter(([path]) => path === '/api/auth/captcha')).toHaveLength(2);
  });

  it('replaces an earlier admin identity and clears account data before entering the new member console', async () => {
    registrationSucceeds = true;
    client.setQueryData(['auth', 'checkAuth', {}], false);
    client.setQueryData(['auth', 'getPermissions', {}], 'admin');
    client.setQueryData(['auth', 'getPermissions', { scope: 'old-admin' }], 'admin');
    client.setQueryData(['auth', 'getIdentity'], { id: 'old-admin', fullName: '旧管理员' });
    const previousAccountKeys = [['dashboard'], ['apps', 'getList'], ['referrals', 1, 1]];
    for (const key of previousAccountKeys) client.setQueryData(key, { account: 'old-admin' });
    const user = open(`/register?invite=${inviteCode}`);
    await fillAndSubmit(user);
    await screen.findByRole('heading', { name: '工作台' });
    expect(client.getQueryData(['auth', 'checkAuth', {}])).toBe(true);
    expect(client.getQueryData(['auth', 'getPermissions', {}])).toBe('member');
    expect(client.getQueryData(['auth', 'getPermissions', { scope: 'old-admin' }])).toBeUndefined();
    expect(client.getQueryData(['auth', 'getIdentity'])).toMatchObject({ id: 'new-member', fullName: '受邀好友', role: 'member' });
    for (const key of previousAccountKeys) expect(client.getQueryData(key)).toBeUndefined();
    expect(submissions).toHaveLength(1);
    expect(vi.mocked(fetch).mock.calls.some(([path]) => path === '/api/auth/login' || path === '/api/auth/logout')).toBe(false);
  });

  it('keeps a cleared normal-query invitation empty when switching routes', async () => {
    window.history.replaceState(null, '', `/?invite=${inviteCode}#/register`);
    const user = open('/register');
    await user.clear(screen.getByRole('textbox', { name: '邀请码（选填）' }));
    await user.click(screen.getByRole('link', { name: /立即登录/ }));
    await user.click(screen.getByRole('link', { name: /立即注册/ }));
    expect((screen.getByRole('textbox', { name: '邀请码（选填）' }) as HTMLInputElement).value).toBe('');
  });

  it('omits the optional invite when registering without a code', async () => {
    const user = open('/register');
    await fillAndSubmit(user);
    await waitFor(() => expect(submissions).toHaveLength(1));
    expect(submissions[0]).not.toHaveProperty('invite_code');
  });
});
