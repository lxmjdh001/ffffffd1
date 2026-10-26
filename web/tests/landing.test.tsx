import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LandingPage } from '../src/landing';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';
import homeMedia from '../src/home-media.json';
import homeContent from '../src/home-content.json';

const config: Config = {
  currency: 'USD', site_name: '星河分发', development: false, registration_open: true,
  recharge_bonus_enabled: false, recharge_bonus_tiers: [], turnstile_site_key: '',
  public_url: 'https://release.example.com', download_base_url: '', download_domain_mode: 'unified', cname_target: '',
  plans: [
    { id: 'free', name: '起步套餐', price_micros: 0, app_limit: 7, download_limit: 1500, storage_bytes: 2 ** 30 },
    { id: 'pro', name: '专业套餐', price_micros: 123450000, app_limit: 317, download_limit: 25000, storage_bytes: 40 * 2 ** 30 },
  ],
  download_price_micros: 3000, max_upload_bytes: 2 ** 30, domain_limit: 3, payment_methods: ['manual'], support_email: '', usdt_rate_micros: 1000000,
};

afterEach(cleanup);
function openLanding(registration_open = true) {
  render(<MemoryRouter initialEntries={['/welcome?invite=invite-code']}><ConfigContext.Provider value={{ config: { ...config, registration_open }, reload: vi.fn() }}><LandingPage /></ConfigContext.Provider></MemoryRouter>);
}

describe('public landing page', () => {
  it('uses the configured support URL for the homepage sales contact', () => {
    const url = 'https://t.me/example_support?start=business';
    render(<MemoryRouter><ConfigContext.Provider value={{ config: { ...config, support_url: url }, reload: vi.fn() }}><LandingPage/></ConfigContext.Provider></MemoryRouter>);
    const hero = document.querySelector('.home-hero')!;
    const contact = within(hero).getByRole('link', { name: '联系商务' });
    expect(contact.getAttribute('href')).toBe(url);
    expect(contact.getAttribute('target')).toBe('_blank');
    expect(contact.getAttribute('rel')).toContain('noopener');
    expect(contact.classList.contains('home-contact-shine')).toBe(true);
  });

  it.each(['', 'javascript:alert(1)', 'https://user:password@example.com'])('keeps contact visible without linking to an unconfigured or unsafe destination %s', support_url => {
    render(<MemoryRouter><ConfigContext.Provider value={{ config: { ...config, support_url }, reload: vi.fn() }}><LandingPage/></ConfigContext.Provider></MemoryRouter>);
    expect(screen.queryByRole('link', { name: '联系商务' })).toBeNull();
    expect(within(document.querySelector('.home-hero')!).getByRole('button', { name: '联系商务' })).toBeTruthy();
  });

  it('shows two 0.05 prices, five free features, and a 100-dollar welcome credit', () => {
    render(<MemoryRouter><ConfigContext.Provider value={{config:{...config,metered_billing:true,download_price_micros:50000,pwa_install_price_micros:50000,plans:[{...config.plans[0],name:'按量计费'}],registration_gift_micros:100e6},reload:vi.fn()}}><LandingPage/></ConfigContext.Provider></MemoryRouter>);
    const pricing=document.getElementById('home-pricing')!;
    expect(pricing.textContent).toContain('仅下载与安装计费');expect(pricing.textContent).toContain('只为实际用量付费');expect(pricing.textContent).not.toContain('注册即赠 $100 用量额度。');expect(within(pricing).getAllByText('$0.05')).toHaveLength(2);expect(within(pricing).getAllByText('免费',{exact:true})).toHaveLength(5);expect(pricing.textContent).not.toContain('1,000');
    expect(pricing.querySelectorAll('.usage-rate-grid article')).toHaveLength(7);expect(within(pricing).getByRole('heading',{name:'应用分流',exact:true})).toBeTruthy();expect(within(pricing).getByRole('heading',{name:'域名安全检测',exact:true})).toBeTruthy();expect(pricing.querySelector('.home-plan')).toBeNull();expect(pricing.textContent).not.toContain('/ 月');
  });
  it('shows configured plan data and keeps the security benefit on the paid plan', () => {
    openLanding();
    const free = screen.getByRole('heading', { name: '起步套餐' }).closest('article')!;
    const paid = screen.getByRole('heading', { name: '专业套餐' }).closest('article')!;
    expect(within(free).getByText('7')).toBeTruthy();
    expect(within(free).getByText('1,500')).toBeTruthy();
    expect(within(free).queryByText('自动检查域名安全')).toBeNull();
    expect(within(paid).getByText('123.45')).toBeTruthy();
    expect(within(paid).getByText('317')).toBeTruthy();
    expect(within(paid).getByText('40.0 GB')).toBeTruthy();
    expect(within(paid).getByText('25,000')).toBeTruthy();
    expect(within(paid).getByText('自动检查域名安全')).toBeTruthy();
    expect(within(paid).getByText(/安全检测需在应用分流中开启/)).toBeTruthy();
    expect(screen.getByText(/按 \$0.003 \/ 次/)).toBeTruthy();
    const authLinks = screen.getAllByRole('link').filter(link => /^\/(register|login)/.test(link.getAttribute('href') || ''));
    expect(authLinks.length).toBeGreaterThan(3);
    for (const link of authLinks) expect(link.getAttribute('href')).toMatch(/^\/(register|login)\?invite=invite-code$/);
  });

  it('sends visitors to login when registration is closed while retaining their invitation', () => {
    openLanding(false);
    expect(screen.getByText('注册暂未开放 · 已有账户可登录')).toBeTruthy();
    expect(screen.queryByRole('link', { name: /开始免费分发/ })).toBeNull();
    for (const link of screen.getAllByRole('link', { name: '登录控制台' })) expect(link.getAttribute('href')).toBe('/login?invite=invite-code');
    expect(screen.getAllByRole('link').some(link => link.getAttribute('href')?.startsWith('/register'))).toBe(false);
  });

  it('presents APK and PWA as separate publishing paths without the removed connection section', () => {
    openLanding();
    expect(document.querySelector('.home-editorial')).toBeTruthy();
    expect(document.querySelector('.home-soft')).toBeNull();
    expect(document.querySelectorAll('.home-story')).toHaveLength(2);
    expect(document.querySelector('.home-story-apk')?.textContent).toContain('上传安装包');
    expect(document.querySelector('.home-story-pwa')?.textContent).toContain('无需下载 APK');
    expect(document.querySelector('.home-connection')).toBeNull();
    expect(document.querySelector('.home-story-pwa')?.textContent).toContain('Web Push');
    expect(document.querySelectorAll('.growth-feature')).toHaveLength(7);
    expect(document.querySelector('.growth-feature-return')?.textContent).toContain('回流页');
    expect(document.querySelector('.home-capability-strip')?.textContent).toContain('广告回传');
    expect(document.querySelector('.growth-feature-cloak')?.textContent).toContain('数百万级投放计划数据沉淀');
    expect(document.querySelector('.growth-feature-cloak')?.textContent).toContain('投放计划验证沉淀');
    expect(screen.getByRole('img', { name: 'APK 下载、PWA 安装与安全检测的卡通手机示意' }).getAttribute('src')).toBe(homeMedia.phoneImage);
    expect(homeMedia.phoneImage.endsWith('.webp')).toBe(true);
    expect(document.querySelector('.home-cartoon')).toBeNull();
  });

  it('groups hero copy beside the phone and pricing copy beside the cards', () => {
    openLanding();
    const hero = document.querySelector('.home-hero-layout')!;
    expect(hero.children).toHaveLength(2);
    expect(hero.children[0].classList.contains('home-hero-copy')).toBe(true);
    expect(hero.children[0].querySelector('.home-hero-aside')).toBeTruthy();
    expect(hero.children[1].classList.contains('home-phone-visual')).toBe(true);
    const pricing = document.getElementById('home-pricing')!;
    expect(pricing.children[0].classList.contains('home-pricing-copy')).toBe(true);
    expect(pricing.children[1].classList.contains('home-pricing-grid')).toBe(true);
  });

  it('presents seven growth capabilities and lets visitors try landing page themes without leaving the page', async () => {
    const user = userEvent.setup(); openLanding();
    const section = document.getElementById('home-growth')!;
    expect(section.querySelectorAll('.growth-feature')).toHaveLength(7);
    for (const item of homeContent.growth.items) expect(within(section).getByRole('heading', { name: item.title })).toBeTruthy();
    expect(section.textContent).toContain('Cloudflare R2');
    expect(section.textContent).toContain('数百万级');
    expect(section.textContent).toContain('70%');
    expect(section.textContent).toContain('关联误封降低目标');
    expect(section.textContent).toContain('二次曝光点击增长目标');
    const themes = within(section).getByRole('group', { name: '落地页 DIY' });
    expect(within(themes).getByRole('button', { name: '极简' }).getAttribute('aria-pressed')).toBe('true');
    await user.click(within(themes).getByRole('button', { name: '品牌' }));
    expect(within(themes).getByRole('button', { name: '品牌' }).getAttribute('aria-pressed')).toBe('true');
    expect(within(themes).getByRole('button', { name: '极简' }).getAttribute('aria-pressed')).toBe('false');
    expect(within(section).getByRole('link', { name: '制作我的落地页' }).getAttribute('href')).toBe('/register?invite=invite-code');
  });

  it('keeps product navigation connected after removing the tools and three-step sections', async () => {
    const descriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'scrollIntoView');
    const scroll = vi.fn();
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', { configurable: true, value: scroll });
    try {
      const user = userEvent.setup(); openLanding();
      expect(document.querySelector('.home-toolbox-details, #home-how, #home-features')).toBeNull();
      expect(screen.queryByRole('button', { name: '如何开始', exact: true })).toBeNull();
      expect(document.querySelector('.home-capability-strip')?.textContent).toContain('自动检查域名安全');
      await user.click(screen.getByRole('button', { name: '产品', exact: true }));
      expect(scroll).toHaveBeenCalled();
      expect(scroll.mock.instances[0]).toBe(document.getElementById('home-solutions'));
    } finally {
      if (descriptor) Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', descriptor);
      else Reflect.deleteProperty(HTMLElement.prototype, 'scrollIntoView');
    }
  });
});
