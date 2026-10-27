import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PWAPushPage } from '../src/pwa-push';
import { blankPushContent, validPWAPushAssetURL, validPWAPushImage, validPushContent } from '../src/pwa-push-content';
import type { PWAApp, PWAPushTemplate } from '../src/pwa-types';

vi.mock('react-admin', () => ({ useNotify: () => vi.fn() }));
const app: PWAApp = { id: 'rich-app', slug: 'rich', name: '轻蓝商店', target_url: 'https://shop.example', description: '', theme_color: '#6373e8', enabled: true, push_enabled: true, icon_url: '', entry_url: 'https://d1.example/p/rich/', launch_url: 'https://d1.example/p/rich/launch', subscription_count: 1, created_at: '2026-10-24T01:00:00Z', updated_at: '2026-10-24T01:00:00Z' };
const template: PWAPushTemplate = { id: 'template-one', name: '每周新品', title: '为你精选', body: '新一周的好物已经上架。', url: 'https://shop.example/new', icon: 'https://images.example/icon.png', image: 'https://images.example/cover.webp', actions: [{ action: 'action_1', title: '查看新品', url: 'https://shop.example/new' }], created_at: app.created_at, updated_at: app.updated_at };
let client: QueryClient; let templates: PWAPushTemplate[]; let writes: { path: string; method: string; body: Record<string, unknown> | FormData; headers: Headers }[]; let requests: URL[]; let failWrite: boolean; let failUpload: boolean;
const descriptors = { showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'), close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close') };
beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } }); templates = [structuredClone(template)]; writes = []; requests = []; failWrite = false; failUpload = false;
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } }); Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const url = new URL(String(input), 'https://console.example'); requests.push(url); const method = options.method || 'GET';
    if (method !== 'GET') {
      const body = options.body instanceof FormData ? options.body : options.body ? JSON.parse(String(options.body)) : {};
      writes.push({ path: url.pathname, method, body, headers: new Headers(options.headers) });
      if (url.pathname === '/api/pwa-push/assets') return failUpload ? new Response(JSON.stringify({ error: '图片上传失败，请重试' }), { status: 500 }) : new Response(JSON.stringify({ url: 'https://d1.example/pwa-push-assets/0123456789abcdef0123456789abcdef.png', width: 800, height: 400 }));
      if (failWrite) return new Response(JSON.stringify({ error: '网络暂时异常，请重试' }), { status: 503 });
      if (url.pathname === '/api/pwa-push/templates' && method === 'POST') { const next = { ...template, ...body, id: 'template-new' }; templates.push(next); return new Response(JSON.stringify(next)); }
      if (url.pathname.startsWith('/api/pwa-push/templates/')) { if (method === 'DELETE') templates = templates.filter(row => row.id !== url.pathname.split('/').at(-1)); else templates = templates.map(row => row.id === url.pathname.split('/').at(-1) ? { ...row, ...body } : row); return new Response(JSON.stringify(templates[0] || {})); }
      return new Response(JSON.stringify({ id: 'task-rich', ...body }));
    }
    if (url.pathname === '/api/pwas') return new Response(JSON.stringify({ data: [app] }));
    if (url.pathname === '/api/pwa-push/templates') { const q = url.searchParams.get('q') || ''; const rows = templates.filter(row => `${row.name} ${row.title} ${row.body}`.includes(q)); const page = Number(url.searchParams.get('page') || 1); return new Response(JSON.stringify({ data: rows.slice((page - 1) * 20, page * 20), total: rows.length, page, per_page: 20 })); }
    if (url.pathname === '/api/pwa-push/tasks') return new Response(JSON.stringify({ data: [], total: 0 }));
    if (url.pathname.startsWith('/api/pwa-push/tasks/')) { if (url.pathname.endsWith('/deliveries')) return new Response(JSON.stringify({ data: [], total: 0 })); return new Response(JSON.stringify({})); }
    throw new Error(`Unexpected request ${method} ${url.pathname}`);
  }));
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks(); for (const [key, descriptor] of Object.entries(descriptors)) { if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor); else Reflect.deleteProperty(HTMLDialogElement.prototype, key); } });
function setup() { render(<QueryClientProvider client={client}><MemoryRouter><PWAPushPage /></MemoryRouter></QueryClientProvider>); return userEvent.setup(); }
async function openTemplates(user: ReturnType<typeof userEvent.setup>) { await user.click(screen.getByRole('tab', { name: '通知模板' })); await screen.findByRole('heading', { name: '每周新品' }); }
async function openComposer(user: ReturnType<typeof userEvent.setup>) { const button = screen.getByRole('button', { name: '新建通知', exact: true }); await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false)); await user.click(button); return within(screen.getByRole('dialog')); }

describe('rich PWA notifications', () => {
  it('copies a reusable template into an editable snapshot and previews images and action labels', async () => {
    const user = setup(); const form = await openComposer(user); const picker = form.getByLabelText('选择通知模板'); await waitFor(() => expect((picker as HTMLSelectElement).disabled).toBe(false)); await user.selectOptions(picker, template.id);
    expect((form.getByLabelText('通知标题') as HTMLInputElement).value).toBe(template.title); expect(form.getByRole('img', { name: '通知大图预览' }).getAttribute('src')).toBe(template.image); expect(form.getByRole('img', { name: '通知图标预览' }).getAttribute('src')).toBe('/pwa-template-assets/push-google.png'); expect(form.getByText('部分设备可能不显示大图或按钮，仍可点击通知正文打开链接。')).toBeTruthy();
    await user.clear(form.getByLabelText('通知标题')); await user.type(form.getByLabelText('通知标题'), '本次专属提醒'); await user.click(form.getByRole('button', { name: '确认发送' })); await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(writes).toHaveLength(1); expect(writes[0].body).toMatchObject({ pwa_id: app.id, template_id: template.id, title: '本次专属提醒', body: template.body, url: template.url, icon: '', image: template.image, actions: template.actions }); expect(templates[0].title).toBe('为你精选');
  });

  it('preserves the selected template from a later list page and can send it for any owned app', async () => {
    templates = [...Array.from({ length: 20 }, (_, index) => ({ ...structuredClone(template), id: `tpl-${index}`, name: `模板 ${index}` })), structuredClone(template)]; const user = setup(); await user.click(screen.getByRole('tab', { name: '通知模板' })); await screen.findByRole('heading', { name: '模板 0' });
    const buttons = screen.getAllByRole('button'); const next = buttons.find(button => button.getAttribute('aria-label') === '下一页' || button.textContent?.includes('下一页'))!; await user.click(next); const heading = await screen.findByRole('heading', { name: '每周新品' }); await user.click(within(heading.closest('article')!).getByRole('button', { name: '使用模板' }));
    const form = within(screen.getByRole('dialog')); expect((form.getByLabelText('选择通知模板') as HTMLSelectElement).value).toBe(template.id); expect((form.getByLabelText('通知标题') as HTMLInputElement).value).toBe(template.title);
  });

  it('snapshots fields cleared after selecting a template and keeps the default app destination explicit', async () => {
    const user = setup(); const form = await openComposer(user); const picker = form.getByLabelText('选择通知模板'); await waitFor(() => expect((picker as HTMLSelectElement).disabled).toBe(false)); await user.selectOptions(picker, template.id); await user.clear(form.getByLabelText('点击后打开的链接')); expect(form.queryByLabelText('通知图标链接')).toBeNull(); await user.clear(form.getByLabelText('通知大图链接')); await user.click(form.getByRole('button', { name: '移除按钮 1' })); await user.click(form.getByRole('button', { name: '确认发送' })); await waitFor(() => expect(writes).toHaveLength(1)); expect(writes[0].body).toMatchObject({ template_id: template.id, url: '', icon: '', image: '', actions: [] }); expect(templates[0].image).toBe(template.image);
  });

  it('creates and edits a template, then requires confirmation before deleting it', async () => {
    const user = setup(); await openTemplates(user); await user.click(screen.getByRole('button', { name: '新建模板', exact: true })); let form = within(screen.getByRole('dialog')); await user.type(form.getByLabelText('模板名称'), '欢迎礼遇'); await user.type(form.getByLabelText('通知标题'), '欢迎加入'); await user.type(form.getByLabelText('通知内容'), '感谢你的订阅。'); await user.click(form.getByRole('button', { name: '保存模板' })); await screen.findByRole('heading', { name: '欢迎礼遇' });
    expect(writes[0].body).toEqual({ name: '欢迎礼遇', title: '欢迎加入', body: '感谢你的订阅。', url: '', icon: '', image: '', actions: [] }); expect(writes[0].headers.get('Idempotency-Key')).toBeTruthy();
    const card = screen.getByRole('heading', { name: '欢迎礼遇' }).closest('article')!; await user.click(within(card).getByRole('button', { name: '编辑' })); form = within(screen.getByRole('dialog')); await user.clear(form.getByLabelText('模板名称')); await user.type(form.getByLabelText('模板名称'), '欢迎礼遇更新'); await user.click(form.getByRole('button', { name: '保存模板' })); await screen.findByRole('heading', { name: '欢迎礼遇更新' }); expect(writes[1]).toMatchObject({ method: 'PUT', path: '/api/pwa-push/templates/template-new' });
    await user.click(screen.getByRole('button', { name: '删除模板 欢迎礼遇更新' })); expect(writes).toHaveLength(2); form = within(screen.getByRole('dialog')); expect(form.getByText('删除后无法恢复。使用过此模板的通知任务仍会按原内容发送。')).toBeTruthy(); await user.click(form.getByRole('button', { name: '确认删除模板' })); await waitFor(() => expect(screen.queryByRole('heading', { name: '欢迎礼遇更新' })).toBeNull()); expect(writes[2]).toMatchObject({ method: 'DELETE', path: '/api/pwa-push/templates/template-new' });
  });

  it('searches template name/content independently of the app filter', async () => {
    const user = setup(); await openTemplates(user); expect(screen.queryByLabelText('筛选应用')).toBeNull(); await user.type(screen.getByLabelText('搜索通知模板'), '不匹配'); expect(await screen.findByText('没有找到匹配的模板')).toBeTruthy(); expect(requests.at(-1)?.searchParams.get('q')).toBe('不匹配'); expect(requests.at(-1)?.searchParams.has('pwa_id')).toBe(false);
  });

  it('requires complete HTTPS action links and allows at most two buttons', async () => {
    const user = setup(); const form = await openComposer(user); await user.type(form.getByLabelText('通知标题'), '活动提醒'); await user.type(form.getByLabelText('通知内容'), '来看看今天的活动。'); const submit = form.getByRole('button', { name: '确认发送' }); await user.click(form.getByRole('button', { name: '添加按钮' })); expect((submit as HTMLButtonElement).disabled).toBe(true); await user.type(form.getByLabelText('按钮 1 文字'), '立即查看'); await user.type(form.getByLabelText('按钮 1 链接'), 'http://shop.example'); expect((submit as HTMLButtonElement).disabled).toBe(true); await user.clear(form.getByLabelText('按钮 1 链接')); await user.type(form.getByLabelText('按钮 1 链接'), 'https://shop.example/offer'); expect((submit as HTMLButtonElement).disabled).toBe(false);
    await user.click(form.getByRole('button', { name: '添加按钮' })); expect((form.getByRole('button', { name: '添加按钮' }) as HTMLButtonElement).disabled).toBe(true); await user.type(form.getByLabelText('按钮 2 文字'), '了解更多'); await user.type(form.getByLabelText('按钮 2 链接'), 'https://shop.example/more'); await user.click(form.getByRole('button', { name: '移除按钮 1' })); expect((form.getByLabelText('按钮 1 文字') as HTMLInputElement).value).toBe('了解更多'); await user.click(submit); await waitFor(() => expect(writes).toHaveLength(1)); expect(writes[0].body).toMatchObject({ actions: [{ action: 'action_1', title: '了解更多', url: 'https://shop.example/more' }] });
  });

  it('uploads images with kind and file using multipart and snapshots the returned public URL', async () => {
    const user = setup(); const form = await openComposer(user); await user.type(form.getByLabelText('通知标题'), '活动提醒'); await user.type(form.getByLabelText('通知内容'), '来看看今天的活动。'); const file = new File(['test-png-bytes'], 'cover.png', { type: 'image/png' }); await user.upload(form.getByLabelText('上传通知大图'), file); await waitFor(() => expect((form.getByLabelText('通知大图链接') as HTMLInputElement).value).toBe('https://d1.example/pwa-push-assets/0123456789abcdef0123456789abcdef.png')); expect(writes[0].body).toBeInstanceOf(FormData); expect((writes[0].body as FormData).get('file')).toBe(file); expect((writes[0].body as FormData).get('kind')).toBe('image'); expect(writes[0].headers.get('Content-Type')).toBeNull(); expect(form.getByRole('img', { name: '通知大图预览' }).getAttribute('src')).toContain('/pwa-push-assets/'); await user.click(form.getByRole('button', { name: '确认发送' })); await waitFor(() => expect(writes).toHaveLength(2)); expect(writes[1].body).toHaveProperty('image', 'https://d1.example/pwa-push-assets/0123456789abcdef0123456789abcdef.png');
  });

  it('keeps a usable editor after an image upload failure and a template save retry', async () => {
    const user = setup(); await openTemplates(user); await user.click(screen.getByRole('button', { name: '新建模板', exact: true })); const form = within(screen.getByRole('dialog')); await user.type(form.getByLabelText('模板名称'), '可重试模板'); await user.type(form.getByLabelText('通知标题'), '你好'); await user.type(form.getByLabelText('通知内容'), '欢迎回来'); failUpload = true; await user.upload(form.getByLabelText('上传通知大图'), new File(['icon'], 'icon.webp', { type: 'image/webp' })); expect(await form.findByText('图片上传失败，请重试')).toBeTruthy(); expect((form.getByRole('button', { name: '保存模板' }) as HTMLButtonElement).disabled).toBe(false); failWrite = true; await user.click(form.getByRole('button', { name: '保存模板' })); await form.findByText('网络暂时异常，请重试'); failWrite = false; await user.click(form.getByRole('button', { name: '保存模板' })); await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(writes[1].headers.get('Idempotency-Key')).toBe(writes[2].headers.get('Idempotency-Key'));
  });

  it('validates image types, sizes, destination protocols, and optional content', () => {
    expect(validPushContent({ ...blankPushContent(), title: '标题', body: '正文' })).toBe(true); expect(validPushContent({ ...blankPushContent(), title: '标题', body: '正文', icon: 'javascript:alert(1)' })).toBe(false); expect(validPushContent({ ...blankPushContent(), title: '标题', body: '正文', actions: [{ action: 'action_1', title: 'A'.repeat(41), url: 'https://example.com' }] })).toBe(false);
    expect(validPWAPushAssetURL('https://images.example/cover.webp')).toBe(true); expect(validPWAPushAssetURL('http://images.example/cover.png')).toBe(false); expect(validPWAPushAssetURL('https://user:pass@example.com/icon.png')).toBe(false); expect(validPWAPushAssetURL('data:image/png;base64,test')).toBe(false);
    expect(validPWAPushImage(new File(['x'], 'a.webp', { type: 'image/webp' }))).toBe(true); expect(validPWAPushImage(new File(['x'], 'a.svg', { type: 'image/svg+xml' }))).toBe(false); expect(validPWAPushImage(new File([new Uint8Array(2 * 1024 * 1024 + 1)], 'large.png', { type: 'image/png' }))).toBe(false); expect(validPWAPushImage(new File([], 'empty.png', { type: 'image/png' }))).toBe(false);
  });
});
