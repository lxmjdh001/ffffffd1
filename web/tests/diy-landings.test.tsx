import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { DIYLandingsPage } from '../src/diy-landings';
import { DIYLandingPreview } from '../src/diy-landing-preview';
import { LandingSourceSelector } from '../src/landing-source-selector';
import { defaultDIYLandingOptions, type DIYLandingPage, type DIYLandingReference } from '../src/diy-landing-types';
import { setLocale } from '../src/i18n';

const response = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
const config = defaultDIYLandingOptions();
const page = (overrides: Partial<DIYLandingPage> = {}): DIYLandingPage => ({
  id: 'page1', name: '活动安装页', draft_html: '<button data-d1-action="primary">安装</button>', draft_config: config,
  draft_version: 1, created_at: '2026-10-26T03:00:00Z', updated_at: '2026-10-26T03:00:00Z', revisions: [], ...overrides,
});
beforeEach(() => { setLocale('zh-CN', false); });
afterEach(() => { cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function workbench(path = '/diy-landings') {
  return render(<MemoryRouter initialEntries={[path]}><Routes><Route path="/diy-landings" element={<DIYLandingsPage/>}/><Route path="/diy-landings/:id/edit" element={<DIYLandingsPage/>}/></Routes></MemoryRouter>);
}

it('opens the code workspace with an empty document and never saves the example placeholder', async () => {
  let payload: Record<string, unknown> | undefined;
  vi.stubGlobal('fetch', vi.fn(async (_input: string, options?: RequestInit) => { payload = JSON.parse(String(options?.body)); return response(page({ name: '空白草稿', draft_html: '' })); }));
  workbench(); const user = userEvent.setup();
  expect((screen.getByRole('textbox', { name: '页面HTML代码' }) as HTMLTextAreaElement).value).toBe('');
  expect(screen.getByText('粘贴代码，查看页面效果')).toBeTruthy();
  await user.type(screen.getByLabelText('落地页名称'), '空白草稿');
  await user.click(screen.getByRole('button', { name: '保存草稿' }));
  await screen.findByText('草稿已保存，正在使用的版本不会改变。');
  expect(payload?.html).toBe('');
  expect(screen.getByRole('button', { name: '我的落地页' })).toBeTruthy();
});

it('saves a draft and publishes a separate pinned version through management endpoints', async () => {
  const requests: { path: string; method?: string; body?: Record<string, unknown>; key?: string | null }[] = [];
  const html = '<h1>新的页面</h1><button data-d1-action="primary">安装</button>';
  vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
    const path = String(input), body = options?.body ? JSON.parse(String(options.body)) : undefined;
    requests.push({ path, method: options?.method, body, key: new Headers(options?.headers).get('Idempotency-Key') });
    if (path.endsWith('/preview')) return response({ html: '<html></html>', preview_url: '/api/diy-landings/preview/preview1', issues: [] });
    if (path.endsWith('/publish')) return response(page({ draft_html: html, latest_published_revision_id: 'revision1', latest_revision_no: 1, revisions: [{ id: 'revision1', page_id: 'page1', revision_no: 1, config, published_at: '2026-10-26T03:00:00Z' }] }));
    return response(page({ draft_html: html }));
  }));
  workbench(); const user = userEvent.setup();
  fireEvent.change(screen.getByLabelText('落地页名称'), { target: { value: '活动安装页' } });
  fireEvent.change(screen.getByRole('textbox', { name: '页面HTML代码' }), { target: { value: html } });
  await waitFor(() => expect(screen.getByTitle('DIY落地页实时预览').getAttribute('src')).toBe('/api/diy-landings/preview/preview1'));
  expect(screen.getByTitle('DIY落地页实时预览').getAttribute('sandbox')).toBe('allow-scripts');
  expect(screen.getByTitle('DIY落地页实时预览').getAttribute('srcdoc')).toBeNull();
  await user.click(screen.getByRole('button', { name: '保存草稿' }));
  await screen.findByText('草稿已保存，正在使用的版本不会改变。');
  expect(requests.some(request => request.path.endsWith('/publish'))).toBe(false);
  expect(requests.find(request => request.path === '/api/diy-landings')?.body?.html).toBe(html);
  await user.click(screen.getByRole('button', { name: '保存并发布' }));
  await screen.findByText('已发布，可在APK或PWA创建和编辑时选择。');
  const published = requests.find(request => request.path.endsWith('/publish'));
  expect(published?.body).toEqual({ draft_version: 1 }); expect(published?.key).toBeTruthy();
  expect(requests.every(request => request.path.startsWith('/api/diy-landings'))).toBe(true);
});

it('keeps the existing revision selected when a newer published revision is available', async () => {
  const requests: string[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string) => {
    const path = String(input); requests.push(path);
    if (path.includes('?')) return response({ data: [page({ latest_published_revision_id: 'revision2', latest_revision_no: 2 })], total: 1 });
    return response(page({ latest_published_revision_id: 'revision2', revisions: [{ id: 'revision2', page_id: 'page1', revision_no: 2, config, published_at: '2026-10-26T04:00:00Z' }, { id: 'revision1', page_id: 'page1', revision_no: 1, config, published_at: '2026-10-26T03:00:00Z' }] }));
  }));
  function Selector() { const [reference, change] = useState<DIYLandingReference>({ page_id: 'page1', revision_id: 'revision1' }); return <MemoryRouter><LandingSourceSelector kind="pwa" value={reference} onChange={next => next && change(next)} hidePreview/><output>{reference.revision_id}</output></MemoryRouter>; }
  render(<Selector/>); const user = userEvent.setup();
  await screen.findByText('此页面有新发布版本，可在使用版本中选择更新。');
  expect((screen.getByLabelText('使用版本') as HTMLSelectElement).value).toBe('revision1');
  expect(screen.getByText('revision1')).toBeTruthy();
  expect(screen.getByRole('link', { name: '打开DIY落地页' }).getAttribute('target')).toBe('_blank');
  expect(screen.getByLabelText('已发布落地页').getAttribute('data-field')).toBe('diy_landing');
  await user.selectOptions(screen.getByLabelText('使用版本'), 'revision2');
  expect(screen.getByText('revision2')).toBeTruthy();
  expect(requests.some(path => path.includes('published=1&target=pwa'))).toBe(true);
});

it('isolates preview loading from the application public entry and cancels stale requests', async () => {
  const previews: { source: string; signal: AbortSignal | null | undefined }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (_input: string, options?: RequestInit) => {
    const data = JSON.parse(String(options?.body)); previews.push({ source: data.html, signal: options?.signal });
    return response({ preview_url: `/api/diy-landings/preview/${previews.length}`, html: '', issues: [] });
  }));
  const view = render(<DIYLandingPreview html="<h1>第一版</h1>" mode="apk"/>);
  await screen.findByTitle('DIY落地页实时预览');
  view.rerender(<DIYLandingPreview html="<h1>第二版</h1>" mode="pwa"/>);
  await waitFor(() => expect(previews).toHaveLength(2));
  expect(previews[0].signal?.aborted).toBe(true);
  expect(screen.getByTitle('DIY落地页实时预览').getAttribute('src')).toBe('/api/diy-landings/preview/2');
});

it('allows keyboard adjustment of the editor split without changing source content', () => {
  workbench(); const divider = screen.getByRole('separator', { name: '调整代码与预览宽度' });
  fireEvent.keyDown(divider, { key: 'ArrowRight' }); expect(divider.getAttribute('aria-valuenow')).toBe('55');
  fireEvent.keyDown(divider, { key: 'Home' }); expect(divider.getAttribute('aria-valuenow')).toBe('50');
  expect((screen.getByRole('textbox', { name: '页面HTML代码' }) as HTMLTextAreaElement).value).toBe('');
});

it('accepts runtime diagnostics only from the current isolated preview and channel', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ preview_url: '/api/diy-landing-preview/diagnostic', channel: 'preview-channel', issues: [] })));
  const issues = vi.fn();
  render(<DIYLandingPreview html="<h1>Preview</h1>" mode="pwa" onIssues={issues}/>);
  const frame = await screen.findByTitle('DIY落地页实时预览') as HTMLIFrameElement;
  const data = { type: 'd1-diy', channel: 'preview-channel', action: 'diagnostic', message: 'Customer script error' };
  fireEvent(window, new MessageEvent('message', { source: window, data }));
  fireEvent(window, new MessageEvent('message', { source: frame.contentWindow, data: { ...data, channel: 'old-preview' } }));
  expect(screen.queryByText(data.message)).toBeNull();
  fireEvent(window, new MessageEvent('message', { source: frame.contentWindow, data }));
  expect(await screen.findByText(data.message)).toBeTruthy();
  expect(issues).toHaveBeenLastCalledWith([{ code: 'runtime', message: data.message, severity: 'error' }]);
});

it('previews application-specific content through its owned revision and cancels older edits', async () => {
  const requests: { path: string; body: Record<string, unknown>; signal?: AbortSignal | null }[] = [];
  vi.stubGlobal('fetch', vi.fn(async (input: string, options?: RequestInit) => {
    requests.push({ path: String(input), body: JSON.parse(String(options?.body)), signal: options?.signal });
    return response({ preview_url: `/api/diy-landing-preview/edit-${requests.length}`, issues: [] });
  }));
  const reference = { page_id: 'private-page', revision_id: 'published-version', content: { 'title-1': '第一个标题', 'primary-label': '立即体验' } };
  const view = render(<DIYLandingPreview reference={reference} mode="pwa"/>);
  await screen.findByTitle('DIY落地页实时预览');
  expect(requests).toHaveLength(1);
  expect(requests[0].body).toEqual({ reference, mode: 'pwa' });
  expect(requests[0].path).toBe('/api/diy-landings/preview');
  const edited = { ...reference, content: { ...reference.content, 'title-1': '第二个标题' } };
  view.rerender(<DIYLandingPreview reference={edited} mode="pwa"/>);
  await waitFor(() => expect(requests).toHaveLength(2));
  expect(requests[0].signal?.aborted).toBe(true);
  expect(requests[1].body).toEqual({ reference: edited, mode: 'pwa' });
  expect(screen.getByTitle('DIY落地页实时预览').getAttribute('sandbox')).toBe('allow-scripts');
  expect(requests.every(request => !request.path.includes('/p/') && !request.path.includes('/a/'))).toBe(true);
});
