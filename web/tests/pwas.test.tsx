import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PWAsPage, PWAEditorPage, validPWAIcon, validPWAURL } from '../src/pwas';
import { defaultReturnFlow } from '../src/return-flow';
import type { PWAApp } from '../src/pwa-types';

vi.mock('react-admin', () => ({ useNotify: () => vi.fn() }));
const app: PWAApp = { id: 'own-app', slug: 'my-shop', name: '我的商店', target_url: 'https://shop.example', description: '随时查看好物', theme_color: '#6373e8', enabled: true, push_enabled: true, icon_url: '', entry_url: 'https://d1.example/p/my-shop', launch_url: 'https://my-shop.apps.example/', subscription_count: 12, created_at: '2026-10-24T01:00:00Z', updated_at: '2026-10-24T02:00:00Z' };
let client: QueryClient;
let rows: PWAApp[];
let writes: { path: string; method: string; body: unknown; headers: Headers }[];
let failIcon: boolean; let failCreate: boolean; let deferred: Promise<void> | undefined;
const dialogDescriptors = { showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'), close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close') };

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } }); rows = [structuredClone(app)]; writes = []; failIcon = false; failCreate = false; deferred = undefined;
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('URL', URL); URL.createObjectURL = vi.fn(() => 'blob:pwa-icon'); URL.revokeObjectURL = vi.fn();
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const path = new URL(String(input), 'https://console.example').pathname; const method = options.method || 'GET';
    if (method !== 'GET') {
      const body = options.body instanceof FormData ? options.body : options.body ? JSON.parse(String(options.body)) : undefined; writes.push({ path, method, body, headers: new Headers(options.headers) });
      if (deferred) await deferred;
      if (method === 'POST' && path === '/api/pwas') { if (failCreate) return new Response(JSON.stringify({ error: '网络暂时异常，请重试' }), { status: 503 }); const result = { ...app, ...body, id: 'created-app' }; rows.push(result); return new Response(JSON.stringify(result), { status: 201 }); }
      if (method === 'POST' && path.endsWith('/icon')) { if (failIcon) return new Response(JSON.stringify({ error: '图标上传暂时失败' }), { status: 503 }); const id = path.split('/')[3]; rows = rows.map(row => row.id === id ? { ...row, icon_url: '/media/new-icon.png' } : row); return new Response(JSON.stringify(rows.find(row => row.id === id))); }
      if (method === 'PUT') { const id = path.split('/')[3]; rows = rows.map(row => row.id === id ? { ...row, ...body } : row); return new Response(JSON.stringify(rows.find(row => row.id === id))); }
      if (method === 'DELETE') { rows = rows.filter(row => row.id !== path.split('/')[3]); return new Response('{}'); }
    }
    if (path.startsWith('/api/pwas/')) return new Response(JSON.stringify(rows.find(row=>row.id===path.split('/')[3])));
    if (path === '/api/pwas') return new Response(JSON.stringify({ data: rows }));
    if (path === '/api/download-domains') return new Response(JSON.stringify({data:[{url:'https://chosen.example',remark:'',source:'platform',audience:'all'}]}));
    throw new Error(`Unexpected request ${method} ${path}`);
  }));
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks(); for (const [key, descriptor] of Object.entries(dialogDescriptors)) { if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor); else Reflect.deleteProperty(HTMLDialogElement.prototype, key); } });
function setup() { render(<QueryClientProvider client={client}><MemoryRouter><Routes><Route path="/" element={<PWAsPage/>}/><Route path="/pwas" element={<PWAsPage/>}/><Route path="/pwas/new" element={<PWAEditorPage/>}/><Route path="/pwas/:id/edit" element={<PWAEditorPage/>}/></Routes></MemoryRouter></QueryClientProvider>); return userEvent.setup(); }
async function openCreate(user: ReturnType<typeof userEvent.setup>) { await user.click(await screen.findByRole('button', { name: '新建 PWA 应用' })); const form = within(await screen.findByRole('form')); await user.type(form.getByLabelText('应用名称'), '新应用'); await user.type(form.getByLabelText('网页 URL'), 'https://new.example'); return form; }

describe('PWA applications', () => {
  it('matches the source template controls and persists all install/access flags',async()=>{
    const user=setup();const form=await openCreate(user);
    expect(form.queryByLabelText('嵌入网页')).toBeNull();
    expect(form.getAllByLabelText('显示桌面二维码')).toHaveLength(1);
    await user.click(form.getByLabelText('兼容 iOS'));
    await user.click(form.getByLabelText('自动下载 APK'));
    await user.click(form.getByLabelText('显示桌面二维码'));
    await user.click(form.getByLabelText('浏览器打开引导'));
    await user.type(form.getByLabelText('浏览器引导文案'),'Open in your browser');
    await user.click(form.getByRole('button',{name:'自定义图片模板'}));
    await user.click(form.getByLabelText('嵌入网页'));
    expect(form.getAllByLabelText('显示桌面二维码')).toHaveLength(1);
    await user.click(form.getByLabelText('显示桌面二维码'));
    await user.click(form.getByRole('button',{name:'创建应用',exact:true}));
    await waitFor(()=>expect(screen.queryByRole('form')).toBeNull());
    expect(writes[0]).toMatchObject({body:{landing_config:{template:'custom',settings:{iosCompatible:false,w2aAutoDownload:true,enableIframeEmbed:true,enableQrcode:true,browserGuideConfig:{enabled:true,message:'Open in your browser'}},storeInfos:[{showQrCode:true}]}}});
  });
  it('opens the right-side domain drawer for the install link',async()=>{
    const user=setup();await screen.findByRole('heading',{name:app.name});
    await user.click(screen.getByRole('button',{name:'复制',exact:true}));
    const drawer=within(await screen.findByRole('dialog'));
    expect(drawer.getByRole('heading',{name:'选择域名复制链接'})).toBeTruthy();
    expect(await drawer.findByText('https://chosen.example/p/my-shop/')).toBeTruthy();
    expect(screen.getByRole('dialog').className).toContain('side');
    expect(writes).toHaveLength(0);
  });
  it('shows owned applications and an install QR code without exposing configuration secrets', async () => {
    const user = setup(); expect(await screen.findByRole('heading', { name: app.name })).toBeTruthy(); expect(screen.getAllByText('12')).toHaveLength(2);
    await user.click(screen.getByRole('button', { name: '安装二维码' })); const dialog = within(screen.getByRole('dialog')); expect(dialog.getByRole('link', { name: app.entry_url }).getAttribute('href')).toBe(app.entry_url); expect(dialog.getByTitle('扫描二维码安装应用')).toBeTruthy();
    expect(screen.queryByText(/endpoint|VAPID|p256dh|auth key/i)).toBeNull(); expect(writes).toHaveLength(0);
  });

  it('retains the created application when icon upload fails, and retries the upload without creating another app', async () => {
    const user = setup(); const form = await openCreate(user); const icon = new File(['icon'], 'icon.png', { type: 'image/png' }); await user.upload(form.getByLabelText('上传应用图标'), icon); failIcon = true;
    await user.click(form.getByRole('button', { name: '创建应用', exact: true })); expect((await form.findByRole('alert')).textContent).toContain('图标上传暂时失败'); expect(form.getByRole('status').textContent).toContain('应用已创建');
    expect(writes.filter(write => write.method === 'POST' && write.path === '/api/pwas')).toHaveLength(1);
    failIcon = false; await user.click(form.getByRole('button', { name: '保存应用' })); await waitFor(() => expect(screen.queryByRole('form')).toBeNull());
    expect(writes.filter(write => write.method === 'POST' && write.path === '/api/pwas')).toHaveLength(1); expect(writes.filter(write => write.path === '/api/pwas/created-app/icon')).toHaveLength(2); expect((writes.find(write => write.body instanceof FormData)!.body as FormData).get('file')).toBeInstanceOf(File);
  });

  it('keeps the same creation key on a failed network request retry', async () => {
    const user = setup(); const form = await openCreate(user); failCreate = true; await user.click(form.getByRole('button', { name: '创建应用', exact: true })); await form.findByRole('alert'); failCreate = false;
    await user.click(form.getByRole('button', { name: '创建应用', exact: true })); await waitFor(() => expect(screen.queryByRole('form')).toBeNull()); const creates = writes.filter(write => write.path === '/api/pwas'); expect(creates).toHaveLength(2); expect(creates[0].headers.get('Idempotency-Key')).toBeTruthy(); expect(creates[1].headers.get('Idempotency-Key')).toBe(creates[0].headers.get('Idempotency-Key'));
  });

  it('prevents duplicate creation and closing while a save is in progress', async () => {
    const user = setup(); const form = await openCreate(user); let release!: () => void; deferred = new Promise(resolve => { release = resolve; }); await user.dblClick(form.getByRole('button', { name: '创建应用', exact: true }));
    expect(writes).toHaveLength(1); await user.click(screen.getByRole('button', { name: '返回应用列表' })); expect(screen.getByRole('form')).toBeTruthy(); release(); await waitFor(() => expect(screen.queryByRole('form')).toBeNull()); expect(rows.filter(row => row.id === 'created-app')).toHaveLength(1);
  });

  it('validates safe website URLs and rejects oversized or unsupported icons before writing', async () => {
    expect(validPWAURL('https://shop.example')).toBe(true); expect(validPWAURL('http://127.0.0.1:5184/test')).toBe(true); for (const value of ['javascript:alert(1)', 'http://shop.example', 'https://user:pass@shop.example', 'https://shop.example:8080', 'not-url']) expect(validPWAURL(value)).toBe(false);
    expect(validPWAIcon(new File(['x'], 'file.svg', { type: 'image/svg+xml' }))).toBe(false); expect(validPWAIcon(new File([new Uint8Array(2 * 1024 * 1024 + 1)], 'file.png', { type: 'image/png' }))).toBe(false);
    const user = setup(); const form = await openCreate(user); await user.upload(form.getByLabelText('上传应用图标'), new File([new Uint8Array(2 * 1024 * 1024 + 1)], 'file.png', { type: 'image/png' })); expect(form.getByRole('alert').textContent).toContain('2 MB'); expect((form.getByRole('button', { name: '创建应用', exact: true }) as HTMLButtonElement).disabled).toBe(true); expect(writes).toHaveLength(0);
  });

  it('preserves settings when pausing an app and requires confirmation to delete only that app', async () => {
    const user = setup(); await user.click(await screen.findByRole('button', { name: '暂停应用' })); await screen.findByText('已暂停'); expect(writes[0]).toMatchObject({ path: '/api/pwas/own-app', method: 'PUT', body: { name: app.name, target_url: app.target_url, enabled: false, push_enabled: true, theme_color: app.theme_color } });
    await user.click(screen.getByRole('button', { name: '删除应用', exact: true })); expect(writes.filter(write => write.method === 'DELETE')).toHaveLength(0); await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '确认删除应用' })); await screen.findByRole('heading', { name: '让网页，走进用户的桌面' }); expect(writes.filter(write => write.method === 'DELETE')).toEqual([expect.objectContaining({ path: '/api/pwas/own-app' })]);
  });
  it('saves return page settings and preserves them when pausing the application', async () => {
    rows[0].return_flow_config = { ...defaultReturnFlow(), enabled: true, triggerMode: 'resume', title: '欢迎回来' };
    const user = setup(); await user.click(await screen.findByRole('button', { name: '编辑', exact: true }));
    const form = within(await screen.findByRole('form')); expect((form.getByLabelText('弹窗标题') as HTMLInputElement).value).toBe('欢迎回来');
    await user.selectOptions(form.getByLabelText('触发方式'), 'both'); await user.click(form.getByRole('button', { name: '保存应用' }));
    await waitFor(() => expect(screen.queryByRole('form')).toBeNull());
    expect(writes[0]).toMatchObject({body:{return_flow_config:{enabled:true,triggerMode:'both',title:'欢迎回来'}}});
    await user.click(screen.getByRole('button', {name:'暂停应用'})); await screen.findByText('已暂停');
    expect(writes[1]).toMatchObject({body:{enabled:false,return_flow_config:{enabled:true,triggerMode:'both',title:'欢迎回来'}}});
  });

  it('opens a dedicated creation route and saves the selected reference landing template', async () => {
    const user=setup();const form=await openCreate(user);expect(screen.queryByRole('dialog')).toBeNull();
    expect(screen.getByRole('heading',{name:'新建 PWA 应用'})).toBeTruthy();
    expect(form.getByRole('button',{name:'Google Play 商店模板'})).toBeTruthy();
    await user.click(form.getByRole('button',{name:'自定义图片模板'}));
    await user.type(form.getByLabelText('按钮文案'),'Install now');
    await user.selectOptions(form.getByLabelText('按钮位置'),'top');
    await user.click(form.getByRole('button',{name:'创建应用',exact:true}));
    await waitFor(()=>expect(screen.queryByRole('form')).toBeNull());
    expect(writes[0]).toMatchObject({body:{landing_config:{template:'custom',storeInfos:[{appName:'新应用',buttonText:'Install now',buttonPosition:'top'}]}}});
  });
  it('preserves localized content, reviews and manifest settings on pause', async () => {
    const {defaultPWALanding}=await import('../src/pwa-landing');const landing=defaultPWALanding();landing.storeInfos[0].appName='Store title';landing.displayMode='fullscreen';landing.storeInfos[0].reviews=[{nickname:'M',comment:'Great',rating:5,commentLanguage:'en-US',sortOrder:0}];rows[0].landing_config=landing;
    const user=setup();await user.click(await screen.findByRole('button',{name:'暂停应用'}));await screen.findByText('已暂停');expect(writes[0]).toMatchObject({body:{landing_config:landing}});
  });

  it('reopens the saved template rather than an older cached draft', async () => {
    const user=setup();await user.click(await screen.findByRole('button',{name:'编辑',exact:true}));let form=within(await screen.findByRole('form'));
    await user.click(form.getByRole('button',{name:'自定义图片模板'}));await user.type(form.getByLabelText('按钮文案'),'Saved custom button');await user.click(form.getByRole('button',{name:'保存应用'}));
    await waitFor(()=>expect(screen.queryByRole('form')).toBeNull());await user.click(screen.getByRole('button',{name:'编辑',exact:true}));form=within(await screen.findByRole('form'));
    expect((form.getByLabelText('按钮文案') as HTMLInputElement).value).toBe('Saved custom button');expect(form.getByRole('button',{name:'自定义图片模板'}).className).toContain('selected');
  });

});
