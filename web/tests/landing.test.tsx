import { cleanup, render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { LandingPage } from '../src/landing';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

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
    expect(screen.getByRole('link', { name: '登录控制台' }).getAttribute('href')).toBe('/login?invite=invite-code');
    expect(screen.getAllByRole('link').some(link => link.getAttribute('href')?.startsWith('/register'))).toBe(false);
  });
});
