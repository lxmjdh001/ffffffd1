import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ActivityPage } from '../src/activity';
import type { Download, Page } from '../src/types';

const sharedUA = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) Chrome/150.0.0.0';
const fixture = (id: string, changes: Partial<Download> = {}): Download => ({
  id, app_id: 'app-a', app_name: `应用 ${id}`, ip: '192.0.2.1', user_agent: sharedUA, ua_blocked: false,
  source: '', device: '桌面', os: 'macOS', browser: 'Chrome', bytes: 1024, amount_micros: 0,
  started_at: '2026-10-24T04:00:00Z', completed_at: '2026-10-24T04:00:02Z', created_at: '2026-10-24T04:00:02Z', attribution_id: '', ...changes,
});
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } });
let client: QueryClient;
let rows: Download[];
let reads: Array<Record<string, string>>;
let writes: Array<{ path: string; method: string; body: unknown }>;
let nextFailure: string | undefined;
let delayMutation: (() => Promise<void>) | undefined;
const dialogDescriptors = {
  showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'),
  close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close'),
};

beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-24T06:00:00Z'));
  rows = [fixture('one'), fixture('two'), fixture('three', { user_agent: 'Different UA' })];
  reads = []; writes = []; nextFailure = undefined; delayMutation = undefined;
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost');
    const method = options?.method || 'GET';
    if (url.pathname === '/api/downloads/apps') return json({ data: [{ id: 'app-a', name: '应用 A', deleted: false }] });
    if (url.pathname === '/api/downloads' && method === 'GET') {
      reads.push(Object.fromEntries(url.searchParams));
      return json({ data: rows, total: 45 });
    }
    if (url.pathname.endsWith('/ua-block')) {
      const body = JSON.parse(String(options?.body));
      writes.push({ path: url.pathname, method, body });
      if (delayMutation) await delayMutation();
      if (nextFailure) { const message = nextFailure; nextFailure = undefined; return json({ error: message }, 500); }
      const selected = rows.find(row => url.pathname === `/api/downloads/${row.id}/ua-block`)!;
      rows = rows.map(row => row.user_agent === selected.user_agent ? { ...row, ua_blocked: body.blocked } : row);
      return json({ blocked: body.blocked });
    }
    throw new Error(`Unexpected request: ${method} ${url.pathname}`);
  }));
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
});
afterEach(() => {
  cleanup(); client.clear(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks();
  for (const [name, descriptor] of Object.entries(dialogDescriptors)) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, name, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name);
  }
});

async function openPage() {
  render(<QueryClientProvider client={client}><ActivityPage /></QueryClientProvider>);
  await screen.findByText('应用 one');
  return userEvent.setup();
}
const getRow = (id: string) => screen.getByText(`应用 ${id}`).closest('tr')!;
async function expandUA(user: ReturnType<typeof userEvent.setup>, id = 'one') {
  const row = getRow(id);
  await user.click(within(row).getByText('查看 UA', { exact: true }));
  expect(row.querySelector('details')!.open).toBe(true);
  return within(row);
}

it('asks for confirmation after expanding the UA and synchronizes identical UA records across cached pages', async () => {
  const user = await openPage();
  const otherPage = ['downloads', '2026-10-23', '2026-10-23', '', 2];
  client.setQueryData<Page<Download>>(otherPage, { data: [fixture('cached')], total: 1 });
  const row = await expandUA(user);
  await expandUA(user, 'two');
  await user.click(row.getByRole('button', { name: '拉黑 UA' }));
  const dialog = within(screen.getByRole('dialog'));
  expect(dialog.getByText(/此操作仅对你的账户生效，其他用户不受影响/)).toBeTruthy();
  expect(writes).toHaveLength(0);
  await user.click(dialog.getByRole('button', { name: '确认拉黑' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(writes).toEqual([{ path: '/api/downloads/one/ua-block', method: 'PUT', body: { blocked: true } }]);
  await waitFor(() => expect(reads).toHaveLength(2));
  expect(within(getRow('one')).getByText('已拉黑')).toBeTruthy();
  expect(within(getRow('two')).getByText('已拉黑')).toBeTruthy();
  expect(within(getRow('three')).queryByText('已拉黑')).toBeNull();
  expect(getRow('one').querySelector('details')!.open).toBe(true);
  expect(client.getQueryData<Page<Download>>(otherPage)?.data[0].ua_blocked).toBe(true);
  expect(client.getQueryState(otherPage)?.isInvalidated).toBe(true);
});

it('keeps a failed operation open and retries without changing the expanded record', async () => {
  const user = await openPage();
  nextFailure = '暂时无法拉黑，请重试';
  await user.click((await expandUA(user)).getByRole('button', { name: '拉黑 UA' }));
  await user.click(screen.getByRole('button', { name: '确认拉黑' }));
  expect((await screen.findByRole('alert')).textContent).toContain('暂时无法拉黑，请重试');
  expect(screen.getByRole('dialog')).toBeTruthy();
  expect(getRow('one').querySelector('details')!.open).toBe(true);
  expect(rows[0].ua_blocked).toBe(false);
  await user.click(screen.getByRole('button', { name: '确认拉黑' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(writes).toHaveLength(2);
  expect(within(getRow('one')).getByText('已拉黑')).toBeTruthy();
});

it('can unblock the UA with the inverse API operation', async () => {
  rows = rows.map(row => ({ ...row, ua_blocked: true }));
  const user = await openPage();
  await user.click((await expandUA(user)).getByRole('button', { name: '解除拉黑' }));
  expect(screen.getByRole('heading', { name: '解除此 UA 的拉黑？' })).toBeTruthy();
  await user.click(screen.getByRole('button', { name: '确认解除' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(writes).toEqual([{ path: '/api/downloads/one/ua-block', method: 'PUT', body: { blocked: false } }]);
  expect(within(getRow('one')).queryByText('已拉黑')).toBeNull();
  expect(within(getRow('one')).getByRole('button', { name: '拉黑 UA' })).toBeTruthy();
});

it('disables empty and whitespace-only UAs without sending a request', async () => {
  rows = [fixture('one', { user_agent: '' }), fixture('two', { user_agent: '   ' })];
  const user = await openPage();
  for (const id of ['one', 'two']) {
    const row = await expandUA(user, id);
    const button = row.getByRole('button', { name: '拉黑 UA' }) as HTMLButtonElement;
    expect(button.disabled).toBe(true);
    expect(row.getByText('无法拉黑未提供 UA 的记录。')).toBeTruthy();
    await user.click(button);
  }
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(writes).toHaveLength(0);
});

it('prevents duplicate requests and closing the dialog while the operation is pending', async () => {
  let finish: (() => void) | undefined;
  delayMutation = () => new Promise<void>(resolve => { finish = resolve; });
  const user = await openPage();
  await user.click((await expandUA(user)).getByRole('button', { name: '拉黑 UA' }));
  const confirm = screen.getByRole('button', { name: '确认拉黑' });
  fireEvent.click(confirm); fireEvent.click(confirm);
  await waitFor(() => expect(writes).toHaveLength(1));
  expect((screen.getByRole('button', { name: '处理中…' }) as HTMLButtonElement).disabled).toBe(true);
  await user.click(screen.getByRole('button', { name: '关闭' }));
  expect(screen.getByRole('dialog')).toBeTruthy();
  finish!();
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(writes).toHaveLength(1);
});

it('retains applied date filters and pagination when refreshing after a block', async () => {
  const user = await openPage();
  await user.click(screen.getByRole('button', { name: '最近 7 天' }));
  await waitFor(() => expect(reads.at(-1)).toEqual({ start_date: '2026-10-18', end_date: '2026-10-24', page: '1' }));
  await user.click(screen.getByRole('button', { name: '下一页' }));
  await waitFor(() => expect(reads.at(-1)?.page).toBe('2'));
  await user.click((await expandUA(user)).getByRole('button', { name: '拉黑 UA' }));
  await user.click(screen.getByRole('button', { name: '确认拉黑' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(reads.at(-1)).toEqual({ start_date: '2026-10-18', end_date: '2026-10-24', page: '2' });
  expect(screen.getByText('2 / 3')).toBeTruthy();
});
