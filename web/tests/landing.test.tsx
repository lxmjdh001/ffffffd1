import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LandingPage } from '../src/landing';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';
import homeMedia from '../src/home-media.json';

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
  it('shows two 0.05 prices, five free features, and a 100-dollar welcome credit', () => {
    render(<MemoryRouter><ConfigContext.Provider value={{config:{...config,metered_billing:true,download_price_micros:50000,pwa_install_price_micros:50000,plans:[{...config.plans[0],name:'按量计费'}],registration_gift_micros:100e6},reload:vi.fn()}}><LandingPage/></ConfigContext.Provider></MemoryRouter>);
    const pricing=document.getElementById('home-pricing')!;
    expect(pricing.textContent).toContain('仅下载与安装计费');expect(pricing.textContent).toContain('只为实际用量付费');expect(pricing.textContent).not.toContain('注册即赠 $100 用量额度。');expect(within(pricing).getAllByText('$0.05')).toHaveLength(2);expect(within(pricing).getAllByText('免费',{exact:true})).toHaveLength(5);expect(pricing.textContent).not.toContain('1,000');
    expect(pricing.querySelectorAll('.usage-rate-grid article')).toHaveLength(7);expect(within(pricing).getByRole('heading',{name:'应用分流',exact:true})).toBeTruthy();expect(within(pricing).getByRole('heading',{name:'域名安全检测',exact:true})).toBeTruthy();expect(pricing.querySelector('.home-plan')).toBeNull();expect(pricing.textContent).not.toContain('/ 月');expect(document.getElementById('home-security')!.textContent).not.toContain('付费会员');
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

  it('presents APK and PWA as separate publishing paths and explains permission-based notifications', () => {
    openLanding();
    expect(document.querySelector('.home-editorial')).toBeTruthy();
    expect(document.querySelector('.home-soft')).toBeNull();
    expect(document.querySelectorAll('.home-story')).toHaveLength(2);
    expect(document.querySelector('.home-story-apk')?.textContent).toContain('上传安装包');
    expect(document.querySelector('.home-story-pwa')?.textContent).toContain('无需下载 APK');
    expect(screen.getByRole('heading', {name:'什么是 PWA？'})).toBeTruthy();
    expect(document.querySelector('.home-push-copy')?.textContent).toContain('用户自主订阅');
    expect(screen.getByText(/iOS 通常需从主屏幕打开/)).toBeTruthy();
    expect(document.querySelectorAll('.home-solution-grid article')).toHaveLength(7);
    expect(document.querySelector('.home-solution-grid')?.textContent).toContain('回流页');
    expect(document.querySelector('.home-solution-grid')?.textContent).toContain('广告回传');
    expect(document.querySelector('.home-solution-grid')?.textContent).toContain('斗篷');
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

  it('highlights automatic domain safety without exposing the internal schedule', () => {
    openLanding();
    const safety = document.getElementById('home-security')!;
    expect(safety).toBeTruthy();
    expect(within(safety).getByRole('heading', {name:'域名安全检测，风险自动退出分流'})).toBeTruthy();
    expect(safety.textContent).toMatch(/Google Web Risk/i);
    expect(safety.textContent).toContain('自动检测');
    expect(safety.textContent).not.toContain('120');
    expect(safety.textContent).not.toContain('分钟');
    expect(safety.textContent).toContain('可用余额或赠送额度');
    expect(safety.textContent).toContain('不扫描主站');
    expect(safety.textContent).toContain('风险域名自动停止参与分流');
    expect(safety.textContent).not.toContain('付费会员');
    expect(document.querySelector('.home-capability-strip button')?.textContent).toBe('自动检查域名安全');
    expect(screen.queryByRole('heading', {name:'自定义域名与免费 SSL'})).toBeNull();
    expect(screen.getByRole('heading', {name:'下载记录与来源统计'})).toBeTruthy();
  });

  it('places the return feature last, opposite the double-width safety feature', () => {
    openLanding();
    const features = document.querySelectorAll('.home-solution-grid article');
    expect(features[0].classList.contains('home-security-feature')).toBe(true);
    expect(features[features.length-1].classList.contains('home-return-feature')).toBe(true);
    expect(features[features.length-1].textContent).toContain('回流页');
    expect(document.querySelector('.home-capability-strip')?.textContent).not.toContain('免费 SSL');
  });
});
