import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { ActivityPage } from '../src/activity';

let client: QueryClient;
let requests: Array<Record<string, string>>;
let appsFail: boolean;
beforeEach(() => {
  vi.useFakeTimers({ toFake: ['Date'] });
  vi.setSystemTime(new Date('2026-10-06T17:00:00Z'));
  requests = [];
  appsFail = false;
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'http://localhost:8088');
    if (url.pathname === '/api/downloads/apps') return new Response(JSON.stringify(appsFail ? { error: '应用列表暂时不可用' } : { data: [
      { id: 'app-one', name: '应用一', deleted: false },
      { id: 'app-two', name: '应用二', deleted: false },
      { id: 'app-deleted', name: '旧应用', deleted: true },
    ] }), { status: appsFail ? 503 : 200, headers: { 'Content-Type': 'application/json' } });
    if (url.pathname !== '/api/downloads') throw new Error(`Unexpected request: ${input}`);
    requests.push(Object.fromEntries(url.searchParams));
    return new Response(JSON.stringify({ data: [], total: 25 }), { headers: { 'Content-Type': 'application/json' } });
  }));
});
afterEach(() => { cleanup(); client.clear(); vi.useRealTimers(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });
async function open() {
  const user = userEvent.setup();
  render(<QueryClientProvider client={client}><ActivityPage /></QueryClientProvider>);
  await screen.findByRole('button', { name: '查询', exact: true });
  return user;
}
const expectRequest = async (start: string, end: string, page = '1', app = '') => {
  await waitFor(() => expect(requests.at(-1)).toEqual({ start_date: start, end_date: end, page, ...(app ? { app_id: app } : {}) }));
  await screen.findByRole('button', { name: '查询', exact: true });
};

it('defaults to today in Shanghai and retains the range while paging, resetting the page on new filters', async () => {
  const user = await open();
  await expectRequest('2026-10-07', '2026-10-07');
  expect(screen.getByRole('heading', { name: '今日下载' })).toBeTruthy();
  expect((screen.getByLabelText('开始日期') as HTMLInputElement).value).toBe('2026-10-07');
  await user.click(screen.getByRole('button', { name: '下一页' }));
  await expectRequest('2026-10-07', '2026-10-07', '2');
  await user.click(screen.getByRole('button', { name: '最近 7 天' }));
  await expectRequest('2026-10-01', '2026-10-07');
  await user.click(screen.getByRole('button', { name: '最近 30 天' }));
  await expectRequest('2026-09-08', '2026-10-07');
  await user.click(screen.getByRole('button', { name: '昨天' }));
  await expectRequest('2026-10-06', '2026-10-06');
  await user.click(screen.getByRole('button', { name: '今天' }));
  await expectRequest('2026-10-07', '2026-10-07');
});

it('applies a custom inclusive 30-day range and blocks invalid or longer queries before requesting records', async () => {
  const user = await open();
  const start = screen.getByLabelText('开始日期'), end = screen.getByLabelText('结束日期');
  fireEvent.change(start, { target: { value: '2026-09-08' } });
  expect(requests).toHaveLength(1);
  await user.click(screen.getByRole('button', { name: '查询', exact: true }));
  await expectRequest('2026-09-08', '2026-10-07');
  const count = requests.length;
  for (const [from, to, message] of [
    ['2026-09-07', '2026-10-07', '单次最多查询 30 天'],
    ['2026-10-07', '2026-10-06', '结束日期不能早于开始日期'],
    ['', '2026-10-07', '请选择开始日期和结束日期'],
  ]) {
    fireEvent.change(start, { target: { value: from } });
    fireEvent.change(end, { target: { value: to } });
    await user.click(screen.getByRole('button', { name: '查询', exact: true }));
    expect((await screen.findByRole('alert')).textContent).toContain(message);
    expect(requests).toHaveLength(count);
  }
});

it('combines application and date filters, preserves them when paging, and resets the page when changing apps', async () => {
  const user = await open();
  const picker = screen.getByRole('combobox', { name: '应用', exact: true });
  await screen.findByRole('option', { name: '应用一' });
  expect(screen.queryByRole('option', { name: /旧应用|已删除/ })).toBeNull();
  expect((picker as HTMLSelectElement).value).toBe('');
  await user.selectOptions(picker, 'app-one');
  expect(requests).toHaveLength(1);
  await user.click(screen.getByRole('button', { name: '查询', exact: true }));
  await expectRequest('2026-10-07', '2026-10-07', '1', 'app-one');
  expect(screen.getByRole('heading', { name: '筛选结果' })).toBeTruthy();
  await user.click(screen.getByRole('button', { name: '下一页' }));
  await expectRequest('2026-10-07', '2026-10-07', '2', 'app-one');
  await user.selectOptions(picker, 'app-two');
  await user.click(screen.getByRole('button', { name: '查询', exact: true }));
  await expectRequest('2026-10-07', '2026-10-07', '1', 'app-two');
  await user.click(screen.getByRole('button', { name: '最近 7 天' }));
  await expectRequest('2026-10-01', '2026-10-07', '1', 'app-two');
  expect(screen.getByText('所选条件暂无下载记录')).toBeTruthy();
  await user.selectOptions(picker, '');
  await user.click(screen.getByRole('button', { name: '查询', exact: true }));
  await expectRequest('2026-10-01', '2026-10-07');
});

it('keeps all-record queries available if the application picker fails and supports retry', async () => {
  appsFail = true;
  const user = await open();
  await screen.findByText('应用列表加载失败，仍可查询全部记录。');
  await expectRequest('2026-10-07', '2026-10-07');
  await user.click(screen.getByRole('button', { name: '最近 7 天' }));
  await expectRequest('2026-10-01', '2026-10-07');
  appsFail = false;
  await user.click(screen.getByRole('button', { name: '重试应用列表' }));
  await screen.findByRole('option', { name: '应用一' });
  expect(screen.queryByText('应用列表加载失败，仍可查询全部记录。')).toBeNull();
});
