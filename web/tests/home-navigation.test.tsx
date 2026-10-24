import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { HomeNavigation } from '../src/home-navigation';
import { setLocale, t } from '../src/i18n';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

afterEach(() => { cleanup(); localStorage.clear(); setLocale('zh-CN', false); vi.restoreAllMocks(); });
function open() {
  render(<MemoryRouter><ConfigContext.Provider value={{ config: { site_name: 'D1 Apk', registration_open: true } as Config, reload: vi.fn() }}><HomeNavigation inviteQuery="?invite=keep-me" startPath="/register?invite=keep-me" /></ConfigContext.Provider></MemoryRouter>);
  return userEvent.setup();
}
it('opens an overlay menu, closes with Escape and restores focus', async () => {
  const user = open();
  const toggle = screen.getByRole('button', { name: '打开导航' });
  expect(toggle.getAttribute('aria-expanded')).toBe('false');
  await user.click(toggle);
  expect(toggle.getAttribute('aria-expanded')).toBe('true');
  const panel = document.getElementById(toggle.getAttribute('aria-controls')!)!;
  expect(within(panel).getAllByRole('button')).toHaveLength(4);
  await user.keyboard('{Escape}');
  expect(toggle.getAttribute('aria-expanded')).toBe('false');
  expect(document.activeElement).toBe(toggle);
  expect(document.getElementById(toggle.getAttribute('aria-controls')!)).toBeNull();
});
it('keeps invitation links in the compact menu and dismisses on section navigation', async () => {
  const user = open();
  await user.click(screen.getByRole('button', { name: '打开导航' }));
  const panel = document.querySelector('.home-menu-panel')!;
  expect(within(panel).getByRole('link', { name: '免费开始' }).getAttribute('href')).toBe('/register?invite=keep-me');
  expect(within(panel).getByRole('link', { name: '登录' }).getAttribute('href')).toBe('/login?invite=keep-me');
  await user.click(within(panel).getByRole('button', { name: '常见问题' }));
  expect(document.querySelector('.home-menu-panel')).toBeNull();
});
it('closes the menu when clicking outside without changing language', async () => {
  const user = open();
  await user.click(screen.getByRole('button', { name: '打开导航' }));
  await user.click(screen.getByRole('button', { name: '语言' }));
  expect(document.querySelector('.home-menu-panel')).toBeNull();
});
it('keeps long translated controls accessible and menu labels reactive', async () => {
  setLocale('fr', false);
  const user = open();
  expect(screen.getByRole('link', { name: t('免费开始') }).getAttribute('title')).toBe(t('免费开始'));
  await user.click(screen.getByRole('button', { name: t('语言') }));
  await user.click(screen.getByRole('menuitemradio', { name: 'Deutsch' }));
  await user.click(screen.getByRole('button', { name: t('打开导航') }));
  expect(within(document.querySelector('.home-menu-panel')!).getByRole('button', { name: t('常见问题') })).toBeTruthy();
});
