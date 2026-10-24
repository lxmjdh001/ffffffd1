import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AuthPage } from '../src/auth';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

vi.mock('react-admin', () => ({ useLogin: () => vi.fn() }));
const inviteCode = '0123456789abcdef';
const config = { site_name: 'D1 Apk', development: true, registration_open: true } as Config;
let submissions: Record<string, unknown>[];

beforeEach(() => {
  window.history.replaceState(null, '', '/');
  submissions = [];
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    if (String(input) === '/api/auth/captcha') return new Response(JSON.stringify({ id: 'challenge-1', image: 'data:image/svg+xml,<svg/>' }));
    if (String(input) === '/api/auth/register') {
      submissions.push(JSON.parse(String(options?.body)));
      return new Response(JSON.stringify({ error: '邀请码无效，请检查后重试' }), { status: 400 });
    }
    throw new Error(`Unexpected request: ${input}`);
  }));
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function open(path: string) {
  render(<ConfigContext.Provider value={{ config, reload: vi.fn() }}><MemoryRouter initialEntries={[path]}><Routes><Route path="/register" element={<AuthPage register />} /><Route path="/login" element={<AuthPage />} /></Routes></MemoryRouter></ConfigContext.Provider>);
  return userEvent.setup();
}

async function fillAndSubmit(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByRole('textbox', { name: '用户名称', exact: true }), '受邀好友');
  await user.type(screen.getByRole('textbox', { name: '邮箱地址', exact: true }), 'invited@example.com');
  await user.type(screen.getByLabelText(/^密码/), 'test-password-123');
  await user.type(screen.getByRole('textbox', { name: /图形验证码/ }), '12345');
  await user.click(screen.getByRole('button', { name: '免费创建账户' }));
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
    await user.click(screen.getByRole('link', { name: /免费注册/ }));
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
  });

  it('keeps a cleared normal-query invitation empty when switching routes', async () => {
    window.history.replaceState(null, '', `/?invite=${inviteCode}#/register`);
    const user = open('/register');
    await user.clear(screen.getByRole('textbox', { name: '邀请码（选填）' }));
    await user.click(screen.getByRole('link', { name: /立即登录/ }));
    await user.click(screen.getByRole('link', { name: /免费注册/ }));
    expect((screen.getByRole('textbox', { name: '邀请码（选填）' }) as HTMLInputElement).value).toBe('');
  });

  it('omits the optional invite when registering without a code', async () => {
    const user = open('/register');
    await fillAndSubmit(user);
    await waitFor(() => expect(submissions).toHaveLength(1));
    expect(submissions[0]).not.toHaveProperty('invite_code');
  });
});
