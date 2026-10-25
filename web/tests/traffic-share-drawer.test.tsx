import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { TrafficPage } from '../src/traffic';

beforeEach(() => {
 Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value: function(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
 Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value: function(this: HTMLDialogElement) { this.removeAttribute('open'); } });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

it('opens a right drawer before copying, searches domains, and copies the selected pool URL', async () => {
 const json = (value: unknown) => new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } });
 vi.stubGlobal('fetch', vi.fn(async (input: string) => {
  if (input.endsWith('/download-domains?usage=traffic')) return json({ data: [
   { url: 'https://facebook-google-down.godownd1.com', remark: '公共一', source: 'platform', audience: 'all' },
   { url: 'https://tiktok-google-down.godownd1.com', remark: '公共二', source: 'platform', audience: 'all' },
   { url: 'https://mine.example.com', remark: '自有域名', source: 'custom', audience: 'owner' },
  ] });
  return json({ paid: false, configured: false, data: [
   { pool: { name: '测试分流', slug: 'my-route', mode: 'random', enabled: true, risk_enabled: false }, links: [], entry_url: 'https://facebook-google-down.godownd1.com/r/my-route' },
   { pool: { name: '另一个分流', slug: 'other-route', mode: 'random', enabled: true, risk_enabled: false }, links: [], entry_url: 'https://facebook-google-down.godownd1.com/r/other-route' },
  ] });
 }));
 const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
 const user = userEvent.setup();
 const clipboard = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
 render(<QueryClientProvider client={client}><MemoryRouter><TrafficPage /></MemoryRouter></QueryClientProvider>);
 await user.click((await screen.findAllByRole('button', { name: '复制入口' }))[0]);
 expect(clipboard).not.toHaveBeenCalled();
 const drawer = screen.getByRole('heading', { name: '选择域名复制分流入口' }).closest('dialog')!;
 expect(drawer.className).toContain('side-drawer');
 await within(drawer).findByText('https://tiktok-google-down.godownd1.com');
 expect(within(drawer).queryByRole('button', { name: '设置' })).toBeNull();
 await user.type(within(drawer).getByRole('textbox', { name: '搜索域名或备注' }), '公共二');
 expect(within(drawer).queryByText('https://facebook-google-down.godownd1.com')).toBeNull();
 await user.click(within(drawer).getByRole('button', { name: '复制' }));
 expect(clipboard).toHaveBeenLastCalledWith('https://tiktok-google-down.godownd1.com/r/my-route');
 expect(within(drawer).getByRole('button', { name: '已复制' })).toBeTruthy();
 await user.click(within(drawer).getByRole('button', { name: '关闭', exact: true }));
 await user.click(screen.getAllByRole('button', { name: '复制入口' })[1]);
 const next = screen.getByRole('heading', { name: '选择域名复制分流入口' }).closest('dialog')!;
 await user.click(within(next).getAllByRole('button', { name: '复制' })[2]);
 expect(clipboard).toHaveBeenLastCalledWith('https://mine.example.com/r/other-route');
 client.clear();
});
