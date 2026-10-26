import { act, cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DIYLandingHelp } from '../src/help-diy-landing';
import { HelpPage, DIYLandingGuidePage } from '../src/help';
import { ConfigContext } from '../src/ui';
import { getDIYLandingAIPrompt, getDIYLandingHelpQuestions, getDIYLandingHelpSteps, getDIYPrimaryButtonExample } from '../src/diy-landing-help-content';
import { setLocale, t } from '../src/i18n';
import type { Config } from '../src/types';

let clipboardDescriptor: PropertyDescriptor | undefined;
const scrollDescriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView');
beforeEach(() => {
  setLocale('zh-CN', false);
  clipboardDescriptor = Object.getOwnPropertyDescriptor(navigator, 'clipboard');
  vi.stubGlobal('fetch', vi.fn(() => { throw new Error('Help must not request an API'); }));
  Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: vi.fn() });
  vi.spyOn(window, 'scrollTo').mockImplementation(() => {});
});
afterEach(() => {
  cleanup(); vi.unstubAllGlobals(); vi.restoreAllMocks(); setLocale('zh-CN', false);
  if (clipboardDescriptor) Object.defineProperty(navigator, 'clipboard', clipboardDescriptor); else Reflect.deleteProperty(navigator, 'clipboard');
  if (scrollDescriptor) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', scrollDescriptor); else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView');
});
function Section() { return <MemoryRouter><DIYLandingHelp/></MemoryRouter>; }
function CurrentRoute() { const location = useLocation(); return <output data-testid="help-route">{location.pathname}{location.search}{location.hash}</output>; }
function Help({ metered = true, initialEntry = '/help' }: { metered?: boolean; initialEntry?: string } = {}) {
  return <MemoryRouter initialEntries={[initialEntry]}><ConfigContext.Provider value={{ config: { metered_billing: metered, cname_target: 'downloads.example', download_price_micros: 10000 } as Config, reload: vi.fn() }}><Routes><Route path="/help" element={<HelpPage/>}/><Route path="/help/diy-landings" element={<DIYLandingGuidePage/>}/></Routes><CurrentRoute/></ConfigContext.Provider></MemoryRouter>;
}
function clipboard(writeText: (value: string) => Promise<void>) {
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
}

it('shows the DIY guide as a whole-card link and removes billing and the old guide cards', () => {
  const { container } = render(<Help/>);
  expect(screen.getByRole('heading', { name: '使用指南', exact: true })).toBeTruthy();
  expect(container.querySelector('.help-grid')?.children).toHaveLength(3);
  expect(screen.getByRole('heading', { name: '01 · 上传与更新' })).toBeTruthy();
  expect(screen.getByRole('heading', { name: '02 · 绑定自己域名' })).toBeTruthy();
  const card = screen.getByRole('link', { name: 'DIY落地页说明' });
  expect(card.classList.contains('help-guide-card')).toBe(true);
  expect(card.getAttribute('href')).toBe('/help/diy-landings');
  expect(within(card).getByRole('heading', { name: 'DIY落地页说明' })).toBeTruthy();
  expect(screen.queryByRole('link', { name: /用量与账单|会员与账单/ })).toBeNull();
  expect(screen.queryByRole('heading', { name: /分享与下载|账户安全与付款|会员、额度与余额|用量与账单/ })).toBeNull();
  expect(screen.getByRole('link', { name: /前往应用管理/ }).getAttribute('href')).toBe('/apps');
  expect(screen.getByRole('link', { name: /管理下载域名/ }).getAttribute('href')).toBe('/domains');
  expect(container.textContent).toContain('downloads.example');
  expect(screen.queryByRole('textbox', { name: 'AI 提示词' })).toBeNull();
  expect(container.querySelector('.help-diy-landing')).toBeNull();
  expect(fetch).not.toHaveBeenCalled();
});

it('also removes the membership billing card for legacy accounts', () => {
  render(<Help metered={false}/>);
  expect(screen.queryByRole('heading', { name: /会员、额度与余额|用量与账单/ })).toBeNull();
  expect(screen.queryByRole('link', { name: /会员与账单|用量与账单/ })).toBeNull();
  expect(screen.getByRole('link', { name: 'DIY落地页说明' })).toBeTruthy();
});

it('opens the full guide from the card and returns to the guide overview', async () => {
  const user = userEvent.setup(); render(<Help/>);
  await user.click(screen.getByRole('link', { name: 'DIY落地页说明' }));
  expect(screen.getByTestId('help-route').textContent).toBe('/help/diy-landings');
  expect(screen.getByRole('heading', { level: 1, name: 'DIY落地页说明' })).toBeTruthy();
  expect(screen.getByRole('textbox', { name: 'AI 提示词' })).toBeTruthy();
  expect(screen.getByRole('link', { name: '打开DIY落地页' }).getAttribute('href')).toBe('/diy-landings');
  expect(screen.getByRole('link', { name: '浏览模版库' }).getAttribute('href')).toBe('/landing-templates');
  expect(window.scrollTo).toHaveBeenCalledWith({ top: 0, left: 0, behavior: 'instant' });
  await user.click(screen.getByRole('link', { name: '使用指南', exact: true }));
  expect(screen.getByTestId('help-route').textContent).toBe('/help');
  expect(screen.queryByRole('textbox', { name: 'AI 提示词' })).toBeNull();
});

it('loads the complete DIY instructions on a direct guide URL', () => {
  const { container } = render(<Help initialEntry="/help/diy-landings"/>);
  expect(screen.getByRole('heading', { level: 1, name: 'DIY落地页说明' })).toBeTruthy();
  expect((screen.getByRole('textbox', { name: 'AI 提示词' }) as HTMLTextAreaElement).value).toBe(getDIYLandingAIPrompt());
  expect(container.querySelectorAll('.help-diy-questions details')).toHaveLength(getDIYLandingHelpQuestions().length);
  expect(container.querySelector('.help-grid')).toBeNull();
  expect(fetch).not.toHaveBeenCalled();
});

it('shows three workflow steps, the exact main-button example and selectable AI requirements', () => {
  const { container } = render(<Section/>);
  expect(container.querySelectorAll('.help-diy-workflow ol>li')).toHaveLength(3);
  for (const step of getDIYLandingHelpSteps()) expect(screen.getByRole('heading', { name: step.title })).toBeTruthy();
  expect(container.querySelector('.help-diy-button-example code')?.textContent).toBe(getDIYPrimaryButtonExample());
  const prompt = screen.getByRole('textbox', { name: 'AI 提示词' }) as HTMLTextAreaElement;
  expect(prompt.readOnly).toBe(true);expect(prompt.value).toBe(getDIYLandingAIPrompt());
  expect(prompt.value).toContain('data-d1-action="primary"');
  expect(prompt.value).toContain('data-d1-bind="primary-label"');
  expect(prompt.value).not.toMatch(/https?:\/\/[^\s"<>]+\.apk(?:[\s"<>]|$)/i);
  expect(prompt.value).not.toMatch(/https?:\/\/[^\s"<>]+\/install\/(?:start|complete)/i);
});

it('copies the exact AI prompt and announces success without calling APIs', async () => {
  const user = userEvent.setup(), writeText = vi.fn(async () => {}); clipboard(writeText); render(<Section/>);
  await user.click(screen.getByRole('button', { name: '复制 AI 提示词' }));
  await screen.findByText('已复制 AI 提示词，可粘贴给 AI。');
  expect(writeText).toHaveBeenCalledExactlyOnceWith(getDIYLandingAIPrompt());
  expect(screen.getByRole('status').getAttribute('aria-live')).toBe('polite');expect(fetch).not.toHaveBeenCalled();
});

it('selects the entire prompt for manual copying when clipboard access fails', async () => {
  const user = userEvent.setup(); clipboard(vi.fn(async () => { throw new Error('Permission denied'); })); render(<Section/>);
  await user.click(screen.getByRole('button', { name: '复制 AI 提示词' }));
  await screen.findByText('复制未完成，已选中全部提示词，请手动复制。');
  const prompt = screen.getByRole('textbox', { name: 'AI 提示词' }) as HTMLTextAreaElement;
  expect(document.activeElement).toBe(prompt);expect(prompt.selectionStart).toBe(0);expect(prompt.selectionEnd).toBe(prompt.value.length);
  expect(prompt.value).toBe(getDIYLandingAIPrompt());expect(fetch).not.toHaveBeenCalled();
});

it('falls back to selection when the Clipboard API is unavailable', async () => {
  const user = userEvent.setup(); Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined }); render(<Section/>);
  await user.click(screen.getByRole('button', { name: '复制 AI 提示词' }));
  await screen.findByText('复制未完成，已选中全部提示词，请手动复制。');
  const prompt = screen.getByRole('textbox', { name: 'AI 提示词' }) as HTMLTextAreaElement;
  expect(prompt.selectionEnd - prompt.selectionStart).toBe(prompt.value.length);
});

it('updates the prompt on language changes and clears old copy status', async () => {
  const user = userEvent.setup(); clipboard(vi.fn(async () => {})); render(<Section/>);
  const original = (screen.getByRole('textbox', { name: 'AI 提示词' }) as HTMLTextAreaElement).value;
  await user.click(screen.getByRole('button', { name: '复制 AI 提示词' }));await screen.findByText('已复制 AI 提示词，可粘贴给 AI。');
  act(() => setLocale('en', false));
  const prompt = screen.getByRole('textbox', { name: t('AI 提示词') }) as HTMLTextAreaElement;
  expect(prompt.value).toBe(getDIYLandingAIPrompt());expect(prompt.value).not.toBe(original);
  await waitFor(() => expect(screen.getByRole('status').textContent).toBe(''));
});

it('ignores a completed clipboard write after the language has changed', async () => {
  let finish: (() => void) | undefined; const user = userEvent.setup();
  clipboard(vi.fn(() => new Promise<void>(resolve => { finish = resolve; })));render(<Section/>);
  await user.click(screen.getByRole('button', { name: '复制 AI 提示词' }));
  expect((screen.getByRole('button', { name: '复制 AI 提示词' }) as HTMLButtonElement).disabled).toBe(true);
  act(() => setLocale('en', false));
  await act(async () => { finish?.(); });
  expect(screen.getByRole('status').textContent).toBe('');
  expect((screen.getByRole('textbox', { name: t('AI 提示词') }) as HTMLTextAreaElement).value).toBe(getDIYLandingAIPrompt());
});

it('exposes questions through accessible native detail controls', async () => {
  const user = userEvent.setup(), { container } = render(<Section/>), questions = getDIYLandingHelpQuestions();
  const details = container.querySelectorAll<HTMLDetailsElement>('.help-diy-questions details');
  expect(details).toHaveLength(questions.length);expect(questions.length).toBeGreaterThan(0);
  expect(details[0].open).toBe(false);await user.click(within(details[0]).getByText(questions[0].question));
  expect(details[0].open).toBe(true);for (const paragraph of questions[0].paragraphs) expect(within(details[0]).getByText(paragraph)).toBeTruthy();
  await user.click(within(details[0]).getByText(questions[0].question));expect(details[0].open).toBe(false);
});
