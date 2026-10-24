import { act, cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { browserLocale, countryLocale, getLocale, initializeLocale, languageOptions, locales, localizedPlanName, setLocale, t, translateContent } from '../src/i18n';
import { LanguageSelector } from '../src/language-selector';
import { LandingPage } from '../src/landing';
import { AppDownloadSettings } from '../src/app-download-settings';
import { UsdtPaymentDetails } from '../src/usdt-payment-details';
import { ConfigContext, money, number } from '../src/ui';
import { applyPageMetadata } from '../src/home-seo';
import type { App, Config, Order } from '../src/types';
import source from '../src/locales/source.json';
import messages from '../src/locales/messages.json';
import home from '../src/home-content.json';

const catalogs = messages as Record<string, Record<string, string>>;
const config = {
  site_name: '客户品牌不翻译', public_url: 'https://release.example.com', registration_open: true,
  plans: [{ id: 'free', name: '客户自定套餐名', price_micros: 0, app_limit: 3, download_limit: 100, storage_bytes: 2 ** 30 }],
  download_price_micros: 3000,
} as Config;
beforeEach(() => { localStorage.removeItem('d1.locale'); setLocale('zh-CN', false); });
afterEach(() => { cleanup(); vi.restoreAllMocks(); localStorage.removeItem('d1.locale'); setLocale('zh-CN', false); });

describe('language detection and preferences', () => {
  it.each([['US','en'],['BR','pt'],['PT','pt'],['DE','de'],['FR','fr'],['KR','ko'],['JP','ja'],['CN','zh-CN'],['TW','zh-TW'],['HK','zh-TW'],['RU','ru']])('maps %s to %s', (country, expected) => {
    expect(countryLocale(country)).toBe(expected);
    initializeLocale(country, ['en-US']);
    expect(getLocale()).toBe(expected);
    expect(localStorage.getItem('d1.locale')).toBeNull();
    expect(document.documentElement.lang).toBe(expected);
  });
  it('uses supported browser languages for unknown countries and English otherwise', () => {
    initializeLocale('XX', ['es-ES', 'ja-JP']); expect(getLocale()).toBe('ja');
    initializeLocale('XX', ['es-ES']); expect(getLocale()).toBe('en');
    expect(browserLocale(['zh-Hant'])).toBe('zh-TW');
    expect(browserLocale(['pt-BR'])).toBe('pt');
  });
  it('never overrides a manual choice on config reload or a new country', () => {
    setLocale('de'); initializeLocale('JP', ['fr-FR']);
    expect(getLocale()).toBe('de');
    expect(localStorage.getItem('d1.locale')).toBe('de');
    setLocale('zh-CN', false); initializeLocale('US'); expect(getLocale()).toBe('de');
  });
  it('ignores invalid stored locales and tolerates unavailable storage', () => {
    localStorage.setItem('d1.locale', 'unsupported'); initializeLocale('FR'); expect(getLocale()).toBe('fr');
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw Error('blocked'); });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw Error('blocked'); });
    expect(() => setLocale('ja')).not.toThrow();
    initializeLocale('KR'); expect(getLocale()).toBe('ko');
  });
});

describe('catalog coverage', () => {
  it.each(locales.filter(locale => locale !== 'zh-CN'))('%s covers every static key and preserves placeholders', locale => {
    expect(Object.keys(catalogs[locale]).sort()).toEqual([...source].sort());
    for (const key of source) {
      const value = catalogs[locale][key];
      expect(value.trim(), `${locale}: ${key}`).not.toBe('');
      expect(value.match(/\{\d+\}/g)?.sort() || [], `${locale}: ${key}`).toEqual(key.match(/\{\d+\}/g)?.sort() || []);
    }
  });
  it('interpolates literal values without interpreting URLs, markup or dollar signs', () => {
    setLocale('en', false);
    const input = '$& <客户> https://example.com/a?x={1}';
    expect(t('删除 {0}', { 0: input })).toBe(`Delete ${input}`);
    expect(t('Unregistered customer content')).toBe('Unregistered customer content');
  });
});

describe('reactive interface', () => {
  it('offers all nine native language names and remembers a selected value', async () => {
    const user = userEvent.setup(); render(<LanguageSelector />);
    await user.click(screen.getByRole('button', { name: '语言' }));
    expect(screen.getAllByRole('menuitemradio').map(option => option.querySelector('[lang]')?.textContent)).toEqual(languageOptions.map(option => option.label));
    await user.click(screen.getByRole('menuitemradio', { name: 'Français' }));
    expect(getLocale()).toBe('fr'); expect(localStorage.getItem('d1.locale')).toBe('fr');
    expect(screen.getByRole('button', { name: 'Langue' })).toBeTruthy();
  });
  it.each(locales)('renders translated homepage content in %s without translating customer data', locale => {
    setLocale(locale, false);
    render(<MemoryRouter><ConfigContext.Provider value={{ config, reload: vi.fn() }}><LandingPage /></ConfigContext.Provider></MemoryRouter>);
    expect(screen.getByRole('heading', { level: 1 }).textContent).toBe(translateContent(home.hero.lines).join(''));
    expect(screen.getByRole('button', { name: t('产品功能') })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '客户自定套餐名' })).toBeTruthy();
    expect(screen.getByText(t(home.faq.items[0].question))).toBeTruthy();
    expect(screen.getAllByText('客户品牌不翻译').length).toBeGreaterThan(0);
    applyPageMetadata(config.site_name, config.public_url, '#/welcome');
    expect(document.title).toContain(t(home.titleSuffix));
    expect(JSON.parse(document.getElementById('home-structured-data')!.textContent!)['@graph'][1].inLanguage).toBe(locale);
  });
  it('changes static option labels without resetting an unsaved app setting', async () => {
    Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.setAttribute('open', ''); } });
    Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.removeAttribute('open'); } });
    const user = userEvent.setup(), client = new QueryClient();
    render(<QueryClientProvider client={client}><LanguageSelector /><AppDownloadSettings app={{ id: 'test', name: '客户应用名称', download_mode: 'direct' } as App} close={vi.fn()} /></QueryClientProvider>);
    await user.click(screen.getByRole('tab', { name: '跳转详情页' }));
    await user.click(screen.getByRole('button', { name: '语言' }));
    await user.click(screen.getByRole('menuitemradio', { name: 'English' }));
    expect(screen.getByRole('tab', { name: 'Open detail page', selected: true })).toBeTruthy();
    expect(screen.getByText(/客户应用名称/)).toBeTruthy(); client.clear();
  });
  it.each(locales)('preserves canonical USDT display and copied amount in %s', async locale => {
    const user = userEvent.setup(); setLocale(locale, false);
    const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
    render(<UsdtPaymentDetails order={{ usdt_units: 499010000, wallet: 'TUnchangedAddress' } as Order} />);
    expect(screen.getByText('499.01')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: t('复制金额') }));
    expect(copy).toHaveBeenLastCalledWith('499.01');
    expect(screen.getByText('TUnchangedAddress')).toBeTruthy();
  });
  it('localizes ordinary numbers without changing stored monetary units', () => {
    act(() => setLocale('de', false));
    expect(number(1234)).toBe('1.234'); expect(money(123450000)).toBe('123,45');
  });
  it('translates system plan labels but leaves customized names unchanged', () => {
    setLocale('en', false);
    expect(localizedPlanName('免费套餐')).toBe('Free plan');
    expect(localizedPlanName('尊享套餐')).toBe('Premium plan');
    expect(localizedPlanName('客户自定义方案')).toBe('客户自定义方案');
  });
});
