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
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

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

it('places PWA and notifications after apps and ad tracking before referrals',()=>{
 open();expect(screen.getByRole('link', {name: 'PWA管理', exact:true}).getAttribute('href')).toBe('/pwas');const links=within(document.querySelector('aside nav')!).getAllByRole('link').map(link=>link.getAttribute('href'));
 expect(links).toEqual(['/apps','/pwas','/pwa-push','/domains','/traffic','/cloaks','/billing','/integrations','/referrals','/activity','/account']);
});

it('expands landing pages into DIY and the template library without navigating the parent item', async () => {
 const user=open();const toggle=screen.getByRole('button',{name:'落地页',exact:true});
 expect(toggle.getAttribute('aria-expanded')).toBe('false');
 expect(screen.queryByRole('link',{name:'DIY落地页',exact:true})).toBeNull();
 await user.click(toggle);
 expect(toggle.getAttribute('aria-expanded')).toBe('true');
 expect(screen.getByRole('link',{name:'DIY落地页',exact:true}).getAttribute('href')).toBe('/diy-landings');
 expect(screen.getByRole('link',{name:'模版库',exact:true}).getAttribute('href')).toBe('/landing-templates');
 expect(screen.getByRole('heading',{name:'应用管理'})).toBeTruthy();
 await user.click(toggle);expect(screen.queryByRole('link',{name:'模版库',exact:true})).toBeNull();
});

it('automatically opens landing navigation for its current child route',()=>{
 open('/landing-templates');
 expect(screen.getByRole('button',{name:'落地页',exact:true}).getAttribute('aria-expanded')).toBe('true');
 expect(screen.getByRole('link',{name:'模版库',exact:true}).getAttribute('aria-current')).toBe('page');
 expect(within(document.querySelector('header')!).getByText('模版库')).toBeTruthy();
});


it('keeps mobile navigation focus inside the open drawer and restores focus on Escape', async () => {
  vi.stubGlobal('matchMedia', vi.fn(() => ({ matches: true, addEventListener: vi.fn(), removeEventListener: vi.fn() })));
  const user = open();
  const sidebar = document.querySelector('aside')!;
  const toggle = screen.getByRole('button', { name: '打开导航' });
  expect(sidebar.hasAttribute('inert')).toBe(true);
  await user.click(toggle);
  expect(sidebar.hasAttribute('inert')).toBe(false);
  expect(toggle.getAttribute('aria-expanded')).toBe('true');
  expect(sidebar.contains(document.activeElement)).toBe(true);
  const controls = sidebar.querySelectorAll<HTMLElement>('a[href], button:not(:disabled)');
  controls[controls.length - 1].focus();
  await user.tab();
  expect(document.activeElement).toBe(controls[0]);
  await user.keyboard('{Escape}');
  expect(sidebar.hasAttribute('inert')).toBe(true);
  expect(document.activeElement).toBe(toggle);
  expect(document.body.style.overflow).toBe('');
});
