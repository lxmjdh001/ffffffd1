import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { AppLayout } from '../src/layout';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

vi.mock('react-admin', async importOriginal => ({
  ...await importOriginal<typeof import('react-admin')>(),
  useGetIdentity: () => ({ data: { id: 'test-member', fullName: '测试用户' } }),
  usePermissions: () => ({ permissions: 'member' }),
  useLogout: () => vi.fn(),
  Notification: () => null,
}));
afterEach(cleanup);

function open(path='/apps') {
  render(<MemoryRouter initialEntries={[path]}><ConfigContext.Provider value={{ config: { site_name: 'D1 Apk', development: false } as Config, reload: vi.fn() }}>
    <AppLayout><Routes><Route path="/" element={<h1>工作台</h1>} /><Route path="/apps" element={<h1>应用管理</h1>} /></Routes></AppLayout>
  </ConfigContext.Provider></MemoryRouter>);
  return userEvent.setup();
}

it('uses the workspace card as the dashboard entry without a duplicate menu item', async () => {
  const user=open();
  const workspace=screen.getByRole('link', { name: '我的工作空间', exact: true });
  expect(workspace.getAttribute('href')).toBe('/');
  expect(workspace.getAttribute('aria-current')).toBeNull();
  expect(screen.queryByRole('link', { name: '工作台', exact: true })).toBeNull();
  await user.click(workspace);
  expect(screen.getByRole('heading', { name: '工作台' })).toBeTruthy();
  expect(workspace.getAttribute('aria-current')).toBe('page');
  expect(workspace.classList.contains('active')).toBe(true);
  expect(within(document.querySelector('header')!).getByText('工作台')).toBeTruthy();
});

it('closes the mobile navigation after using the workspace entry and keeps other navigation intact', async () => {
  const user=open();
  await user.click(screen.getByRole('button', { name: '打开导航' }));
  expect(document.querySelector('aside')?.classList.contains('open')).toBe(true);
  await user.click(screen.getByRole('link', { name: '我的工作空间', exact: true }));
  expect(document.querySelector('aside')?.classList.contains('open')).toBe(false);
  expect(screen.queryByRole('button', { name: '关闭导航' })).toBeNull();
  await user.click(screen.getByRole('link', { name: /应用管理/ }));
  expect(screen.getByRole('heading', { name: '应用管理' })).toBeTruthy();
  expect(screen.getByRole('link', { name: '我的工作空间', exact: true }).getAttribute('aria-current')).toBeNull();
});
