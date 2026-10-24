import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BillingPage } from '../src/billing';
import { ConfigContext } from '../src/ui';
import type { Config, Dashboard, Ledger, Order, Plan, User } from '../src/types';
import { alipayQuote } from '../src/alipay';

const free: Plan = { id: 'free', name: '免费套餐', price_micros: 0, app_limit: 10, download_limit: 1000, storage_bytes: 10 * 2 ** 30 };
const account: User = { id: 'test-member', email: 'member@example.com', name: '测试用户', role: 'member', disabled: false, balance_micros: 0, held_micros: 0, pro_until: null, created_at: '2026-10-01T00:00:00Z' };
const dashboard: Dashboard = {
  user: account, plan: free, usage: { period: '2026-10', free_used: 0, free_reserved: 0, paid_used: 0, spent_micros: 0 },
  quota_limit: 1000, quota_used: 0, quota_reserved: 0, quota_remaining: 1000, quota_renews_at: null, app_count: 0, storage_bytes: 0, total_downloads: 0, active_domains: 0, quota_resets_at: '2026-11-01T00:00:00Z', series: [],
};
const order: Order = {
  id: 'test-order', user_id: account.id, method: 'manual', amount_micros: 1000000000, bonus_micros: 500000000,
  status: 'pending', trade_id: null, pay_url: '', usdt_units: 0, wallet: '', rate_micros: 0,
  expires_at: '2099-10-08T00:00:00Z', created_at: '2026-10-07T00:00:00Z', paid_at: null,
};
const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
let config: Config;
let client: QueryClient;
let submissions: unknown[];
let existingOrders: Order[];
let existingLedger: Ledger[];
let createdOrder: Order | undefined;
const dialogMethods = {
  showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'),
  close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close'),
};

beforeEach(() => {
  config = {
    currency: 'USD', site_name: 'D1 Apk', development: true, registration_open: true,
    recharge_bonus_enabled: true, recharge_bonus_tiers: [{ threshold_micros: 200000000, percent: 15 }, { threshold_micros: 500000000, percent: 35 }, { threshold_micros: 1000000000, percent: 50 }],
    turnstile_site_key: '', public_url: 'http://localhost:8088', download_base_url: '', download_domain_mode: 'unified', cname_target: 'dns.d1apk.com',
    plans: [free, { ...free, id: 'pro', name: '尊享套餐', price_micros: 99000000, app_limit: 500, storage_bytes: 30 * 2 ** 30, download_limit: 10000 }], download_price_micros: 500,
    max_upload_bytes: 2 ** 30, domain_limit: 5, payment_methods: ['manual'], support_email: '', usdt_rate_micros: 1000000,
  };
  submissions = []; existingOrders = []; existingLedger = []; createdOrder = undefined;
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.stubGlobal('scrollTo', vi.fn());
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const path = String(input).split('?')[0];
    if (path === '/api/me') return json({ user: account, csrf_token: 'test-csrf' });
    if (path === '/api/dashboard') return json(dashboard);
    if (path === '/api/orders' && options?.method === 'POST') {
      const body = JSON.parse(String(options.body)); submissions.push(body);
      createdOrder = { ...order, method: body.method, pay_url: 'https://payments.example/submit.php?order=test-order', usdt_units: 1000007123, wallet: 'TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t' };
      if (body.method === 'alipay') {
        const amount = Math.round(Number(body.amount) * 1e6);
        const quote = alipayQuote(amount, config.alipay_cny_rate_micros ?? 6650000)!;
        Object.assign(createdOrder, { amount_micros: amount, bonus_micros: 0, payment_currency: 'CNY', alipay_rate_micros: config.alipay_cny_rate_micros ?? 6650000, fee_micros: quote.feeMicros, pay_cny_cents: quote.payCNYCents });
      }
      return json(createdOrder);
    }
    if (path === '/api/orders') return json({ data: existingOrders, total: existingOrders.length });
    if (path === '/api/ledger') return json({ data: existingLedger, total: existingLedger.length });
    if (path === `/api/orders/${order.id}`) return json(createdOrder || order);
    throw new Error(`Unexpected request: ${input}`);
  }));
});
afterEach(() => {
  cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks();
  for (const [name, descriptor] of Object.entries(dialogMethods)) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, name, descriptor);
    else Reflect.deleteProperty(HTMLDialogElement.prototype, name);
  }
});

function openBilling() {
  render(<AdminContext queryClient={client}><ConfigContext.Provider value={{ config, reload: vi.fn() }}><BillingPage /></ConfigContext.Provider></AdminContext>);
  return userEvent.setup();
}

describe('recharge promotion', () => {
  it('previews the 5% CNY charge, submits only principal and opens the locked Alipay order', async () => {
    config.payment_methods = ['alipay', 'manual']; config.alipay_cny_rate_micros = 6650000;
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    expect(screen.getByText('¥698.25')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: '创建充值订单' }));
    await waitFor(() => expect(submissions).toEqual([{ amount: '100', method: 'alipay' }]));
    await screen.findByRole('heading', { name: '充值订单' });
    expect(screen.getByText('¥ 698.25')).toBeTruthy();
    expect(screen.getByText(/充值本金 \$100.00 · 手续费 5%（\$5.00）/)).toBeTruthy();
    expect(screen.getByRole('link', { name: '前往支付宝付款' }).getAttribute('href')).toBe('https://payments.example/submit.php?order=test-order');
    expect(screen.getByText(/订单汇率：1 USDT = ¥6.65/)).toBeTruthy();
  });
  it('identifies referral credits in the ledger and retains fractional-cent rewards', async () => {
    existingLedger = [{ id: 'referral-ledger', kind: 'referral_reward', amount_micros: 20200, balance_micros: 20200, note: '二级邀请返利 · 好友充值 1.010000 USD', reference: 'referral:test-order:2', created_at: '2026-10-24T03:00:00Z' }];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '余额流水' }));
    expect(await screen.findByText('邀请返利')).toBeTruthy();
    expect(screen.getByText('二级邀请返利 · 好友充值 1.010000 USD')).toBeTruthy();
    expect(screen.getByText('+ $0.0202')).toBeTruthy();
    expect(screen.getByText('$0.0202')).toBeTruthy();
  });

  it('shows the two compact plans with the balance-card recharge action', async () => {
    const user = openBilling();
    const freeCard = (await screen.findByRole('heading', { name: '免费套餐' })).closest('section')!;
    const paidCard = screen.getByRole('heading', { name: '尊享套餐' }).closest('section')!;
    expect(within(freeCard).getByText('10.0 GB')).toBeTruthy();
    expect(within(freeCard).getByText('1,000')).toBeTruthy();
    expect(within(freeCard).getByText(/仅注册首月赠送/)).toBeTruthy();
    expect(within(paidCard).getByText('$99')).toBeTruthy();
    expect(within(paidCard).getByText('500')).toBeTruthy();
    expect(within(paidCard).getByText('30.0 GB')).toBeTruthy();
    expect(within(paidCard).getByText('10,000')).toBeTruthy();
    const recharge = screen.getByRole('button', { name: '充值余额' });
    expect(recharge.closest('section')?.textContent).toContain('可用余额');
    await user.click(recharge);
    expect(screen.getByRole('heading', { name: '充值账户余额' })).toBeTruthy();
  });

  it('offers Alipay when configured without enabling the legacy WeChat channel', async () => {
    config.payment_methods = ['alipay', 'wechat', 'manual'];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const channels = within(screen.getByRole('group', { name: '支付方式' }));
    expect(channels.getAllByRole('button')).toHaveLength(3);
    expect((channels.getByRole('button', { name: 'USDT' }) as HTMLButtonElement).disabled).toBe(true);
    expect(channels.getByRole('button', { name: '支付宝' }).getAttribute('aria-pressed')).toBe('true');
    expect(channels.queryByText(/微信/)).toBeNull();
  });

  it('submits USDT principal only and shows exact on-chain payment instructions', async () => {
    config.payment_methods = ['usdt', 'manual'];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const channels = within(screen.getByRole('group', { name: '支付方式' }));
    expect(channels.getAllByRole('button')).toHaveLength(3);
    expect((channels.getByRole('button', { name: '支付宝' }) as HTMLButtonElement).disabled).toBe(true);
    expect(channels.getByRole('button', { name: 'USDT' }).getAttribute('aria-pressed')).toBe('true');
    await user.click(screen.getByRole('button', { name: /^\$1000/ }));
    await user.click(screen.getByRole('button', { name: '创建充值订单' }));
    await waitFor(() => expect(submissions).toEqual([{ amount: '1000', method: 'usdt' }]));
    await screen.findByRole('heading', { name: '充值订单' });
    const payment = within(screen.getByRole('dialog'));
    expect(payment.getByText('仅 TRON / TRC20')).toBeTruthy();
    expect(payment.getByText('1,000.007123')).toBeTruthy();
    expect(payment.getByText('TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t')).toBeTruthy();
  });

  it('opens recharge when the member clicks insufficient balance during upgrade', async () => {
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: /开通尊享套餐/ }));
    const insufficient = screen.getByRole('button', { name: '余额不足，请先充值' }) as HTMLButtonElement;
    expect(insufficient.disabled).toBe(false);
    await user.click(insufficient);
    expect(screen.queryByRole('heading', { name: '开通尊享套餐' })).toBeNull();
    const dialog = within(screen.getByRole('dialog'));
    expect(dialog.getByRole('heading', { name: '充值账户余额' })).toBeTruthy();
    expect((dialog.getByRole('spinbutton', { name: '充值金额（美元）' }) as HTMLInputElement).value).toBe('100');
    expect(dialog.queryByText('活动赠送')).toBeNull();
    expect(submissions).toEqual([]);
  });

  it('offers four quick amounts, marks the gift tier and submits only the real payment amount', async () => {
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const dialog = within(screen.getByRole('dialog'));
    expect(dialog.getAllByRole('button', { name: /^\$/ }).map(button => button.textContent)).toEqual(['$100', '$200赠送15%', '$500赠送35%', '$1000赠送50%']);
    expect(dialog.queryByText('活动赠送')).toBeNull();
    await user.click(dialog.getByRole('button', { name: /^\$1000/ }));
    expect(dialog.getByText('+ $500.00')).toBeTruthy();
    expect(dialog.getByText('$1,500.00')).toBeTruthy();
    await user.click(dialog.getByRole('button', { name: '创建充值订单' }));
    await waitFor(() => expect(submissions).toEqual([{ amount: '1000', method: 'manual' }]));
    await screen.findByRole('heading', { name: '充值订单' });
    const payment = within(screen.getByRole('dialog'));
    expect(payment.getByText('实付金额')).toBeTruthy();
    expect(payment.getByText('$ 1,000.00')).toBeTruthy();
    expect(payment.getByText('+ $500.00')).toBeTruthy();
    expect(payment.getByText('$1,500.00')).toBeTruthy();
  });

  it('previews custom amounts at the threshold without rounding away fractional-cent bonuses', async () => {
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const dialog = within(screen.getByRole('dialog'));
    const amount = dialog.getByRole('spinbutton', { name: '充值金额（美元）' });
    for (const [value, bonus, total] of [['199.99', '', ''], ['200.01', '+ $30.0015', '$230.0115'], ['500.01', '+ $175.0035', '$675.0135'], ['1000.01', '+ $500.005', '$1,500.015'], ['2000', '+ $1,000.00', '$3,000.00']]) {
      await user.clear(amount); await user.type(amount, value);
      if (bonus) { expect(dialog.getByText(bonus)).toBeTruthy(); expect(dialog.getByText(total)).toBeTruthy(); }
      else expect(dialog.queryByText('活动赠送')).toBeNull();
    }
  });

  it('hides an inactive campaign but preserves the gift promised by an existing order', async () => {
    config.recharge_bonus_enabled = false;
    existingOrders = [order];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const dialog = within(screen.getByRole('dialog'));
    expect(dialog.getByRole('button', { name: '$1000', exact: true })).toBeTruthy();
    await user.click(dialog.getByRole('button', { name: '$1000', exact: true }));
    expect(dialog.queryByText('活动赠送')).toBeNull();
    expect(dialog.queryByText(/按最高适用档位/)).toBeNull();
    await user.click(dialog.getByRole('button', { name: '取消' }));
    await user.click(await screen.findByRole('button', { name: /查看详情/ }));
    const payment = within(screen.getByRole('dialog'));
    expect(payment.getByText('+ $500.00')).toBeTruthy();
    expect(payment.getByText('$1,500.00')).toBeTruthy();
  });
});
