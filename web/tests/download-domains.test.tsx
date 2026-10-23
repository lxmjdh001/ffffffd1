import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppDetailPage } from '../src/apps';
import { AdminSettingsPage } from '../src/admin';
import { authProvider } from '../src/api';
import { ConfigContext } from '../src/ui';
import type { Config, SettingsResponse } from '../src/types';

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
const app = { id: 'test-app', name: '测试应用', slug: 'abc123', file_name: 'test.apk', platform: 'Android', version: '1.0.0', description: '', enabled: true, blocked: false, size: 1024, downloads: 0, sha256: 'test-hash', updated_at: '2026-10-21T00:00:00Z' };
let assignedBase: string;
let customDomain: boolean;
let role: 'member' | 'admin';
let client: QueryClient;
let saved: SettingsResponse | undefined;
let settings: SettingsResponse;

beforeEach(() => {
  assignedBase = ''; customDomain = false; role = 'member'; saved = undefined;
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.stubGlobal('scrollTo', vi.fn());
  settings = {
    settings: { id: 1, site_name: 'D1 Apk', download_domain_mode: 'unified', download_base_url: '', free_download_base_url: '', paid_download_base_url: '', registration_open: true, download_price_micros: 3000, max_upload_bytes: 500 << 20, domain_limit: 10, usdt_rate_micros: 1000000, support_email: '', updated_at: '', recharge_bonus_enabled: true, recharge_bonus_tiers: [{ threshold_micros: 200000000, percent: 15 }] },
    plans: [{ id: 'free', name: '免费套餐', price_micros: 0, app_limit: 10, download_limit: 1000, storage_bytes: 10 * 2 ** 30 }, { id: 'pro', name: '尊享套餐', price_micros: 99000000, app_limit: 500, download_limit: 10000, storage_bytes: 30 * 2 ** 30 }],
    runtime: { public_url: 'https://d1apk.com', cname_target: 'dns.d1apk.com' },
  };
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const path = String(input).split('?')[0];
    if (path === '/api/me') return json({ user: { id: 'test-customer', name: '测试用户', role }, csrf_token: 'test-csrf' });
    if (path === '/api/download-domain') return json({ mode: 'separate', plan_id: 'free', download_base_url: assignedBase });
    if (path === '/api/apps/test-app') return json(app);
    if (path === '/api/domains') return json({ data: customDomain ? [{ id: 'custom', hostname: 'customer.example.com', verified_at: new Date().toISOString() }] : [], total: customDomain ? 1 : 0 });
    if (path === '/api/admin/settings' && options?.method === 'PUT') { saved = JSON.parse(String(options.body)); return json({ ...settings, ...saved }); }
    if (path === '/api/admin/settings') return json(settings);
    throw new Error(`Unexpected request: ${input}`);
  }));
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function open(path = '/apps/test-app') {
  render(<MemoryRouter initialEntries={[path]}><AdminContext queryClient={client} authProvider={authProvider}><ConfigContext.Provider value={{ config: { public_url: 'https://d1apk.com', download_base_url: 'https://d1apk.com' } as Config, reload: vi.fn() }}><Routes><Route path="/apps/:id" element={<AppDetailPage />} /><Route path="/admin/settings" element={<AdminSettingsPage />} /></Routes></ConfigContext.Provider></AdminContext></MemoryRouter>);
  return userEvent.setup();
}

it('does not manufacture website download links when no assigned domain exists', async () => {
  open();
  await screen.findByRole('heading', { name: '测试应用', level: 1 });
  await waitFor(() => expect(vi.mocked(fetch).mock.calls.some(([url]) => String(url) === '/api/download-domain')).toBe(true));
  expect(screen.getByText(/尚未分配下载域名/)).toBeTruthy();
  expect(screen.queryByRole('link', { name: /直接下载/ })).toBeNull();
  expect(screen.queryByText('https://d1apk.com/a/abc123')).toBeNull();
});

it.each(['https://free.example.com', 'https://paid.example.com'])('uses the server-assigned domain %s for shared links', async base => {
  assignedBase = base;
  open();
  const link = await screen.findByRole('link', { name: /直接下载/ });
  expect(link.getAttribute('href')).toBe(`${base}/a/abc123`);
  expect(screen.getByText(`${base}/a/abc123`)).toBeTruthy();
  expect(screen.queryByText('https://d1apk.com/a/abc123')).toBeNull();
});

it('continues to offer a verified customer domain without a shared domain', async () => {
  customDomain = true;
  open();
  const link = await screen.findByRole('link', { name: /直接下载/ });
  expect(link.getAttribute('href')).toBe('https://customer.example.com/a/abc123');
});

it('allows administrators to switch between unified and separate domain assignment', async () => {
  role = 'admin';
  const user = open('/admin/settings');
  const mode = await screen.findByRole('combobox', { name: '分配方式' });
  expect(screen.getByRole('textbox', { name: /统一下载域名/ })).toBeTruthy();
  await user.selectOptions(mode, 'separate');
  await user.type(screen.getByRole('textbox', { name: /免费客户下载域名/ }), 'free.example.com');
  await user.type(screen.getByRole('textbox', { name: /付费客户下载域名/ }), 'https://paid.example.com');
  expect(screen.queryByRole('textbox', { name: /统一下载域名/ })).toBeNull();
  await user.click(screen.getByRole('button', { name: '保存全部设置' }));
  await waitFor(() => expect(saved?.settings.download_domain_mode).toBe('separate'));
  expect(saved?.settings.free_download_base_url).toBe('free.example.com');
  expect(saved?.settings.paid_download_base_url).toBe('https://paid.example.com');
  await user.selectOptions(mode, 'unified');
  expect(screen.getByRole('textbox', { name: /统一下载域名/ })).toBeTruthy();
});
