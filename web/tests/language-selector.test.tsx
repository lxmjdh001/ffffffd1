import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it } from 'vitest';
import { LanguageSelector } from '../src/language-selector';
import { setLocale, getLocale } from '../src/i18n';
afterEach(() => { cleanup(); localStorage.clear(); setLocale('zh-CN', false); });
it('opens a compact menu with the current language selected and closes on Escape', async () => {
  const user=userEvent.setup(); render(<LanguageSelector />);
  const trigger=screen.getByRole('button', {name:'语言'});
  await user.click(trigger);
  expect(screen.getAllByRole('menuitemradio')).toHaveLength(9);
  expect(screen.getByRole('menuitemradio',{name:'简体中文'}).getAttribute('aria-checked')).toBe('true');
  expect(document.activeElement).toBe(screen.getByRole('menuitemradio',{name:'简体中文'}));
  await user.keyboard('{Escape}');
  expect(screen.queryByRole('menu')).toBeNull();
  expect(document.activeElement).toBe(trigger);
});
it('supports keyboard navigation, selection and a remembered preference', async () => {
  const user=userEvent.setup(); render(<LanguageSelector />);
  screen.getByRole('button',{name:'语言'}).focus();
  await user.keyboard('{ArrowDown}{End}{Home}{ArrowDown}{Enter}');
  expect(getLocale()).toBe('pt');
  expect(localStorage.getItem('d1.locale')).toBe('pt');
  expect(screen.queryByRole('menu')).toBeNull();
});
it('dismisses on outside clicks without changing language', async () => {
  const user=userEvent.setup();render(<><LanguageSelector /><button>Outside</button></>);
  await user.click(screen.getByRole('button',{name:'语言'}));
  await user.click(screen.getByRole('button',{name:'Outside'}));
  expect(screen.queryByRole('menu')).toBeNull();expect(getLocale()).toBe('zh-CN');
});
it('keeps different selector instances isolated and leaves no floating menu after unmount', async () => {
  const user=userEvent.setup();const page=render(<><LanguageSelector /><LanguageSelector /></>);
  const triggers=screen.getAllByRole('button',{name:'语言'});
  await user.click(triggers[0]);await user.click(triggers[1]);
  expect(screen.getAllByRole('menu')).toHaveLength(1);
  expect(triggers[0].getAttribute('aria-expanded')).toBe('false');
  page.unmount();expect(screen.queryByRole('menu')).toBeNull();
});
