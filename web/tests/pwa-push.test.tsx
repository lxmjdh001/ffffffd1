import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { PWAPushPage, validPWAPushURL, validPWASchedule } from '../src/pwa-push';
import type { PWAApp, PWAPushTask } from '../src/pwa-types';

vi.mock('react-admin', () => ({ useNotify: () => vi.fn() }));
const app: PWAApp = { id: 'own-app', slug: 'shop', name: '我的商店', target_url: 'https://shop.example', description: '', theme_color: '#6373e8', enabled: true, push_enabled: true, icon_url: '', entry_url: 'https://d1.example/p/shop', launch_url: 'https://shop.apps.example/', subscription_count: 12, created_at: '2026-10-24T01:00:00Z', updated_at: '2026-10-24T01:00:00Z' };
const task: PWAPushTask = { id: 'task-one', pwa_id: app.id, pwa_name: app.name, title: '欢迎回来', body: '本周新品已经上架。', url: app.launch_url, schedule_type: 'scheduled', scheduled_at: '2026-11-01T02:00:00Z', delay_minutes: 0, target_countries: [], status: 'queued', total_count: 12, sent_count: 8, failed_count: 1, displayed_count: 7, clicked_count: 2, created_at: '2026-10-24T01:00:00Z', completed_at: null };
let client: QueryClient; let apps: PWAApp[]; let tasks: PWAPushTask[]; let writes: { path: string; method: string; body: Record<string, unknown>; headers: Headers }[]; let requests: URL[]; let failWrite: boolean; let deferred: Promise<void> | undefined;
const dialogDescriptors = { showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'), close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close') };

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } }); apps = [structuredClone(app)]; tasks = [structuredClone(task)]; writes = []; requests = []; failWrite = false; deferred = undefined;
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } }); Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const url = new URL(String(input), 'https://console.example'); requests.push(url); const method = options.method || 'GET';
    if (method !== 'GET') { const body = options.body ? JSON.parse(String(options.body)) : {}; writes.push({ path: url.pathname, method, body, headers: new Headers(options.headers) }); if (deferred) await deferred; if (failWrite) return new Response(JSON.stringify({ error: '网络暂时异常，请重试' }), { status: 503 }); if (method === 'DELETE') tasks = tasks.map(row => row.id === url.pathname.split('/').at(-1) ? { ...row, status: 'cancelled' } : row); return new Response(JSON.stringify({ ...task, ...body })); }
    if (url.pathname === '/api/pwas') return new Response(JSON.stringify({ data: apps }));
    if (url.pathname === '/api/pwa-push/templates') return new Response(JSON.stringify({ data: [], total: 0 }));
    if (url.pathname === '/api/pwa-push/tasks') return new Response(JSON.stringify({ data: tasks, total: tasks.length }));
    if (url.pathname === '/api/pwa-push/tasks/task-one') return new Response(JSON.stringify(tasks[0]));
    if (url.pathname === '/api/pwa-push/subscriptions') return new Response(JSON.stringify({ data: [{ id: 'sub-one', pwa_id: app.id, pwa_name: app.name, country: 'US', status: 'active', browser: 'Chrome', device: 'Android', created_at: task.created_at, last_seen_at: task.created_at, endpoint: 'never-show-endpoint', keys: { auth: 'never-show-auth-key' } }], total: 1 }));
    if (url.pathname.endsWith('/deliveries')) return new Response(JSON.stringify({ data: [{ id: 'delivery-one', task_id: task.id, subscription_id: 'sub-one', country: 'US', status: 'sent', attempts: 1, last_error: 'never-show-internal-error', created_at: task.created_at, sent_at: task.created_at, displayed_at: task.created_at, clicked_at: null }], total: 1 }));
    throw new Error(`Unexpected request ${method} ${url.pathname}`);
  }));
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks(); for (const [key, descriptor] of Object.entries(dialogDescriptors)) { if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor); else Reflect.deleteProperty(HTMLDialogElement.prototype, key); } });
function setup(path = '/pwa-push') { render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[path]}><PWAPushPage /></MemoryRouter></QueryClientProvider>); return userEvent.setup(); }
async function composer(user: ReturnType<typeof userEvent.setup>) { const button = await screen.findByRole('button', { name: '新建通知' }); await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false)); await user.click(button); const form = within(screen.getByRole('dialog')); await user.type(form.getByLabelText('通知标题'), '优惠提醒'); await user.type(form.getByLabelText('通知内容'), '你的专属优惠已经准备好。'); return form; }

describe('PWA notification management', () => {
  it('sends a notification only for the selected owned app and defaults the destination without exposing keys', async () => {
    const user = setup(); const form = await composer(user); expect(form.getByText('发送给当前符合条件且已允许通知的用户。')).toBeTruthy(); await user.click(form.getByRole('button', { name: '确认发送' })); await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(writes[0].body).toEqual({ pwa_id: 'own-app', title: '优惠提醒', body: '你的专属优惠已经准备好。', schedule_type: 'now', target_countries: [] }); expect(writes[0].headers.get('Idempotency-Key')).toBeTruthy();
  });

  it('keeps an idempotency key on retry and prevents a double click while sending', async () => {
    const user = setup(); const form = await composer(user); failWrite = true; await user.click(form.getByRole('button', { name: '确认发送' })); await form.findByRole('alert'); failWrite = false; let release!: () => void; deferred = new Promise(resolve => { release = resolve; }); await user.dblClick(form.getByRole('button', { name: '确认发送' })); expect(writes).toHaveLength(2); expect(writes[1].headers.get('Idempotency-Key')).toBe(writes[0].headers.get('Idempotency-Key')); await user.click(form.getByRole('button', { name: '关闭' })); expect(screen.getByRole('dialog')).toBeTruthy(); release(); await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });

  it('requires a future schedule and converts local device time into an absolute timestamp', async () => {
    const user = setup(); const form = await composer(user); await user.click(form.getByRole('radio', { name: '定时发送' })); const date = form.getByLabelText('计划发送时间'); const submit = form.getByRole('button', { name: '创建定时通知' }); expect((submit as HTMLButtonElement).disabled).toBe(true);
    const past = '2020-01-01T12:00'; await user.type(date, past); expect((submit as HTMLButtonElement).disabled).toBe(true); const future = new Date(Date.now() + 7 * 86400000); const local = `${future.getFullYear()}-${String(future.getMonth() + 1).padStart(2, '0')}-${String(future.getDate()).padStart(2, '0')}T12:00`; await user.clear(date); await user.type(date, local); expect((submit as HTMLButtonElement).disabled).toBe(false); await user.click(submit); await waitFor(() => expect(writes).toHaveLength(1)); expect(writes[0].body.scheduled_at).toBe(new Date(local).toISOString()); expect(writes[0].body.schedule_type).toBe('scheduled');
  });

  it('clearly scopes a persistent first-subscription rule to future subscribers and accepts a zero-minute delay', async () => {
    const user = setup(); const form = await composer(user); await user.click(form.getByRole('radio', { name: '首次订阅后发送' })); expect(form.getByText('此规则持续生效，仅面向之后新增的订阅用户。填写 0 表示订阅后立即发送。')).toBeTruthy(); const delay = form.getByLabelText('订阅后延迟（分钟）'); await user.clear(delay); await user.type(delay, '43201'); expect((form.getByRole('button', { name: '保存订阅通知规则' }) as HTMLButtonElement).disabled).toBe(true); await user.clear(delay); await user.type(delay, '0'); await user.click(form.getByRole('button', { name: '保存订阅通知规则' })); await waitFor(() => expect(writes).toHaveLength(1)); expect(writes[0].body).toMatchObject({ schedule_type: 'first_subscribe', delay_minutes: 0 }); expect(writes[0].body).not.toHaveProperty('scheduled_at');
  });

  it('shows subscription device information while keeping endpoint and authentication data out of the UI', async () => {
    const user = setup('/pwa-push?app=own-app'); await user.click(screen.getByRole('tab', { name: '订阅用户' })); expect(await screen.findByText('Android')).toBeTruthy(); expect(screen.getByText('Chrome')).toBeTruthy(); expect(screen.getByText('已订阅')).toBeTruthy(); expect(screen.queryByText(/never-show/)).toBeNull(); expect(requests.find(url => url.pathname.endsWith('/subscriptions'))?.searchParams.get('pwa_id')).toBe('own-app'); expect(writes).toHaveLength(0);
  });

  it('requires confirmation before cancelling a task and updates its status', async () => {
    const user = setup(); await user.click(await screen.findByRole('button', { name: '取消发送', exact: true })); expect(writes).toHaveLength(0); const form = within(screen.getByRole('dialog')); expect(form.getByText('取消后，尚未发送的消息将停止发送。已发送的消息无法撤回。')).toBeTruthy(); await user.click(form.getByRole('button', { name: '确认取消发送' })); expect(await screen.findByText('已取消')).toBeTruthy(); expect(writes[0]).toMatchObject({ path: '/api/pwa-push/tasks/task-one', method: 'DELETE' });
  });

  it('shows per-delivery display/click outcomes without revealing subscription secrets or internal errors', async () => {
    const user = setup(); await user.click(await screen.findByRole('button', { name: '查看详情' })); const dialog = within(screen.getByRole('dialog')); expect(dialog.getByText('已发送表示消息已交付通知服务；展示与点击以设备回报为准。')).toBeTruthy(); expect(await dialog.findByRole('cell', { name: '已发送' })).toBeTruthy(); expect(dialog.getByRole('columnheader', { name: '点击时间' })).toBeTruthy(); expect(screen.queryByText(/never-show/)).toBeNull(); expect(writes).toHaveLength(0);
  });

  it('shows the actual icon, large image and separate button destinations in task details', async () => {
    tasks[0] = { ...tasks[0], icon: 'https://images.example/icon.png', image: 'https://images.example/offer.png', actions: [{ action: 'action_1', title: '查看优惠', url: 'https://shop.example/offer' }, { action: 'action_2', title: '联系我们', url: 'https://shop.example/contact' }] };
    const user = setup(); await user.click(await screen.findByRole('button', { name: '查看详情' })); const dialog = within(screen.getByRole('dialog')); expect(dialog.getByRole('img', { name: '通知图标' }).getAttribute('src')).toBe('https://images.example/icon.png'); expect(dialog.getByRole('img', { name: '通知大图' }).getAttribute('src')).toBe('https://images.example/offer.png'); expect(dialog.getByText('查看优惠')).toBeTruthy(); expect(dialog.getByText('https://shop.example/offer')).toBeTruthy(); expect(dialog.getByText('联系我们')).toBeTruthy(); expect(dialog.getByText('https://shop.example/contact')).toBeTruthy(); expect(writes).toHaveLength(0);
  });

  it('disables notification creation without an enabled app and validates delay and HTTPS destinations', async () => {
    apps = [{ ...app, push_enabled: false }]; const user = setup(); await screen.findByText('欢迎回来'); expect((screen.getByRole('button', { name: '新建通知' }) as HTMLButtonElement).disabled).toBe(true); await user.click(screen.getByRole('button', { name: '新建通知' })); expect(screen.queryByRole('dialog')).toBeNull();
    expect(validPWAPushURL('https://shop.example/path')).toBe(true); for (const value of ['http://shop.example', 'javascript:alert(1)', 'https://user:pass@shop.example']) expect(validPWAPushURL(value)).toBe(false);
    expect(validPWASchedule('first_subscribe', '', '0')).toBe(true); for (const value of ['-1', '1.5', '1e2', '43201']) expect(validPWASchedule('first_subscribe', '', value)).toBe(false); expect(validPWASchedule('scheduled', '2020-01-01T12:00', '')).toBe(false); expect(validPWASchedule('scheduled', '2099-01-01T12:00', '')).toBe(false);
  });
});
