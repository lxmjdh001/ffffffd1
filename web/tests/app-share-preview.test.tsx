import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppDetailPage } from '../src/apps';
import type { App, Domain } from '../src/types';

const assigned = vi.hoisted(() => ({ base: 'https://platform.example.com' }));
vi.mock('../src/download-domain', () => ({
  useDownloadDomain: () => ({ data: { download_base_url: assigned.base } }),
}));
vi.mock('react-admin', () => ({ useNotify: () => vi.fn() }));

const clients: QueryClient[] = [];
const app: App = {
  id: 'app1', user_id: 'user1', name: '测试应用', slug: 'my-brand',
  version: '1.0.0', description: '', platform: 'Android', file_name: 'sample.apk',
  size: 100, sha256: 'test-sha256', enabled: true, download_mode: 'direct',
  blocked: false, downloads: 0, created_at: '2026-10-24T00:00:00Z',
  updated_at: '2026-10-24T00:00:00Z',
};

beforeEach(() => {
  assigned.base = 'https://platform.example.com';
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', {
    configurable: true, value: function (this: HTMLDialogElement) { this.setAttribute('open', ''); },
  });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', {
    configurable: true, value: function (this: HTMLDialogElement) { this.removeAttribute('open'); },
  });
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach(client => client.clear());
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function readyDomain(hostname: string): Domain {
  return { id: hostname, hostname, token: '', ssl_status: 'ready',
    verified_at: new Date().toISOString(), last_error: '', created_at: app.created_at };
}

function renderDetail(domains: Domain[], downloadMode: App['download_mode'] = 'direct') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  clients.push(client);
  client.setQueryData(['app', app.id], { ...app, download_mode: downloadMode });
  client.setQueryData(['domain-links'], { data: domains, total: domains.length });
  const urls = [assigned.base, ...domains.map(domain => `https://${domain.hostname}`)].filter(Boolean);
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    if (String(input) !== '/api/download-domains') throw new Error(`Unexpected request: ${input}`);
    return new Response(JSON.stringify({ data: urls.map(url => ({
      url, remark: '', source: url === assigned.base ? 'platform' : 'custom', audience: 'owner',
    })) }), { headers: { 'Content-Type': 'application/json' } });
  }));
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/apps/app1']}>
    <Routes><Route path="/apps/:id" element={<AppDetailPage />} /></Routes>
  </MemoryRouter></QueryClientProvider>);
  return screen.getByRole('heading', { name: '分享链接' }).closest('section')!;
}

it('shows one preview with 100 customer domains while the drawer can still select and copy any domain', async () => {
  const domains = Array.from({ length: 100 }, (_, i) => readyDomain(`customer-${i + 1}.example.com`));
  const user = userEvent.setup();
  const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
  const preview = renderDetail(domains);
  expect(preview.querySelectorAll('.link-card')).toHaveLength(1);
  expect(within(preview).getByText('https://platform.example.com/a/my-brand')).toBeTruthy();
  expect(within(preview).getAllByRole('button', { name: '复制链接' })).toHaveLength(1);
  expect(screen.queryByText('https://customer-100.example.com/a/my-brand')).toBeNull();

  await user.click(within(preview).getByRole('button', { name: '复制链接' }));
  await screen.findByText('https://customer-100.example.com');
  expect(document.querySelector('dialog')?.className).toContain('side-drawer');
  expect(document.querySelectorAll('.share-domain-card')).toHaveLength(101);
  await user.type(screen.getByRole('textbox', { name: '搜索域名或备注' }), 'customer-100');
  expect(document.querySelectorAll('.share-domain-card')).toHaveLength(1);
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '复制' }));
  expect(clipboard).toHaveBeenCalledWith('https://customer-100.example.com/a/my-brand');
  await user.click(screen.getByRole('button', { name: '关闭' }));
  expect(preview.querySelectorAll('.link-card')).toHaveLength(1);
});

it('falls back to the first ready, recently verified customer domain when no platform domain is assigned', () => {
  assigned.base = '';
  const preview = renderDetail([
    { ...readyDomain('pending.example.com'), ssl_status: 'pending' },
    { ...readyDomain('unverified.example.com'), verified_at: null },
    { ...readyDomain('stale.example.com'), verified_at: new Date(Date.now() - 49 * 3600000).toISOString() },
    readyDomain('first.example.com'), readyDomain('second.example.com'),
  ]);
  expect(preview.querySelectorAll('.link-card')).toHaveLength(1);
  expect(within(preview).getByText('https://first.example.com/a/my-brand')).toBeTruthy();
  expect(within(preview).getByRole('link', { name: '直接下载' }).getAttribute('href')).toBe('https://first.example.com/a/my-brand');
  expect(within(preview).getByRole('link', { name: '查看详情' }).getAttribute('href')).toBe('https://first.example.com/a/my-brand/info');
});

it('keeps the existing notice when no download domain is available', () => {
  assigned.base = '';
  const preview = renderDetail([]);
  expect(preview.querySelectorAll('.link-card')).toHaveLength(0);
  expect(within(preview).getByText('尚未分配下载域名，请联系管理员配置，或绑定并验证自己的域名。')).toBeTruthy();
  expect(within(preview).queryByRole('button', { name: '复制链接' })).toBeNull();
});

it('preserves the details-page action for applications using info mode', () => {
  const preview = renderDetail([readyDomain('customer.example.com')], 'info');
  expect(preview.querySelectorAll('.link-card')).toHaveLength(1);
  expect(within(preview).getByRole('link', { name: '打开详情页' }).getAttribute('href')).toBe('https://platform.example.com/a/my-brand');
  expect(within(preview).getByRole('link', { name: '查看详情' }).getAttribute('href')).toBe('https://platform.example.com/a/my-brand/info');
});
