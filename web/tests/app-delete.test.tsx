import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppsPage } from '../src/apps';
import type { App } from '../src/types';

const notify = vi.hoisted(() => vi.fn());
vi.mock('react-admin', async () => {
  const { useQuery } = await import('@tanstack/react-query');
  return {
    useNotify: () => notify,
    useGetList: () => {
      const query = useQuery({ queryKey: ['apps', 'getList'], queryFn: async () => (await fetch('/api/apps')).json() });
      return { ...query, data: query.data?.data, total: query.data?.total };
    },
  };
});

const clients: QueryClient[] = [];
let apps: App[];
let deletionStatus: number;
let cleanupPending: boolean;
let fetcher: ReturnType<typeof vi.fn>;
beforeEach(() => {
  apps = ['first', 'second'].map(id => ({ id, name: `${id} 应用`, slug: id, platform: 'Android', enabled: true, size: 100 } as App));
  deletionStatus = 200;
  cleanupPending = false;
  fetcher = vi.fn(async (input: string, options?: RequestInit) => {
    if (options?.method === 'DELETE') {
      if (deletionStatus !== 200) return new Response(JSON.stringify({ error: '删除失败，请稍后重试' }), { status: deletionStatus });
      apps = apps.filter(app => `/api/apps/${app.id}` !== String(input));
      return new Response(JSON.stringify({ ok: true, file_cleanup_pending: cleanupPending }));
    }
    return new Response(JSON.stringify({ data: apps, total: apps.length }));
  });
  vi.stubGlobal('fetch', fetcher);
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
  notify.mockClear();
});

async function showApps() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  render(<QueryClientProvider client={client}><MemoryRouter><AppsPage /></MemoryRouter></QueryClientProvider>);
  await screen.findByRole('button', { name: '删除应用 first 应用' });
}
const deletes = () => fetcher.mock.calls.filter(([, options]) => options?.method === 'DELETE');

it('adds deletion to the list and requires confirmation before sending a permanent DELETE', async () => {
  const user = userEvent.setup();
  await showApps();
  await user.click(screen.getByRole('button', { name: '删除应用 first 应用' }));
  expect(screen.getByRole('heading', { name: '确定永久删除这个应用？' })).toBeTruthy();
  expect(screen.getByText(/此操作无法恢复/)).toBeTruthy();
  expect(deletes()).toHaveLength(0);
  await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: '取消' }));
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(deletes()).toHaveLength(0);

  await user.click(screen.getByRole('button', { name: '删除应用 first 应用' }));
  await user.click(screen.getByRole('button', { name: '确认永久删除' }));
  await waitFor(() => expect(screen.queryByRole('button', { name: '删除应用 first 应用' })).toBeNull());
  expect(screen.getByRole('button', { name: '删除应用 second 应用' })).toBeTruthy();
  expect(deletes()).toHaveLength(1);
  expect(fetcher).toHaveBeenCalledWith('/api/apps/first', expect.objectContaining({ method: 'DELETE' }));
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(notify).toHaveBeenCalledWith('应用、安装包及上传图片已永久删除', { type: 'success' });
});

it('keeps the app and confirmation open on server failure and permits retry', async () => {
  deletionStatus = 500;
  const user = userEvent.setup();
  await showApps();
  await user.click(screen.getByRole('button', { name: '删除应用 first 应用' }));
  await user.click(screen.getByRole('button', { name: '确认永久删除' }));
  await screen.findByRole('alert');
  expect(screen.getByText('删除失败，请稍后重试')).toBeTruthy();
  expect(screen.getByRole('button', { name: '删除应用 first 应用' })).toBeTruthy();
  expect(screen.getByRole('dialog')).toBeTruthy();
  expect(notify).not.toHaveBeenCalled();
  deletionStatus = 200;
  await user.click(screen.getByRole('button', { name: '确认永久删除' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(deletes()).toHaveLength(2);
});

it('warns when the app is deleted but package cleanup needs a retry', async () => {
  cleanupPending = true;
  const user = userEvent.setup();
  await showApps();
  await user.click(screen.getByRole('button', { name: '删除应用 first 应用' }));
  await user.click(screen.getByRole('button', { name: '确认永久删除' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  expect(notify).toHaveBeenCalledWith('应用已永久删除，安装包或图片清理暂未完成，后台将重试清理', { type: 'warning' });
});
