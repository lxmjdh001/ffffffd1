import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { PageDesigner, type PageDesign } from '../src/page-designer';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

vi.mock('../src/download-domain', () => ({ useDownloadDomain: () => ({ data: { download_base_url: 'http://localhost:8088' } }) }));
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
it('previews drafts separately and publishes only when saved', async () => {
 const design: PageDesign = { layout: 'store', title: '原始标题', developer: '', summary: '', description: '', icon: '', screenshots: [], tags: [], button_text: '下载应用', theme_color: '#246b55', background_color: '#f4f7f5', show_description: true, show_screenshots: true, show_file_info: true, show_downloads: true };
 let published: PageDesign | undefined;
 const requests: string[] = [];
 vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
  requests.push(String(input));
  let body: unknown = {};
  if (String(input).endsWith('/preview')) body = { html: '<html><body>预览</body></html>' };
  else if (options?.method === 'PUT') { published = JSON.parse(String(options.body)); body = { design: published }; }
  else body = { app: { id: 'app1', slug: 'abc123', name: '测试应用' }, design, defaults: design };
  return new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
 }));
 const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
 render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/apps/app1/design']}><ConfigContext.Provider value={{ config: { development: true, public_url: 'http://localhost:8088' } as Config, reload: vi.fn() }}><Routes><Route path="/apps/:id/design" element={<PageDesigner />} /><Route path="/apps" element={<h1>应用列表</h1>} /></Routes></ConfigContext.Provider></MemoryRouter></QueryClientProvider>);
 const user = userEvent.setup();
 const title = await screen.findByLabelText('展示名称');
 await user.clear(title); await user.type(title, '编辑后的标题');
 await waitFor(() => expect(requests.some(p => p.endsWith('/preview'))).toBe(true));
 expect(published).toBeUndefined();
 const frame = screen.getByTitle('应用详情页实时预览'); expect(frame.getAttribute('sandbox')).toBe('');
 await user.click(screen.getByRole('button', { name: '桌面' }));
 expect(frame.parentElement?.className).toContain('desktop');
 expect(screen.getByRole('link', { name: /查看已发布页面/ }).getAttribute('href')).toBe('http://localhost:8088/a/abc123/info');
 await user.click(screen.getByRole('button', { name: '保存并发布' }));
 await screen.findByRole('heading', { name: '应用列表' });
 expect(published?.title).toBe('编辑后的标题');
 client.clear();
});
