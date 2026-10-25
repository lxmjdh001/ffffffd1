import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { BillingPage } from '../src/billing';
import { ConfigContext } from '../src/ui';
import type { Config, Dashboard, Ledger, Order, Plan, User } from '../src/types';

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
let paymentChecks: number;
const dialogMethods = {
  showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'),
  close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close'),
};

beforeEach(() => {
  account.gift_balance_micros=0;account.gift_held_micros=0;
  config = {
    currency: 'USD', site_name: 'D1 Apk', development: true, registration_open: true,
    recharge_bonus_enabled: true, recharge_bonus_tiers: [{ threshold_micros: 200000000, percent: 15 }, { threshold_micros: 500000000, percent: 35 }, { threshold_micros: 1000000000, percent: 50 }],
    turnstile_site_key: '', public_url: 'http://localhost:8088', download_base_url: '', download_domain_mode: 'unified', cname_target: 'dns.d1apk.com',
    plans: [free, { ...free, id: 'pro', name: '尊享套餐', price_micros: 99000000, app_limit: 500, storage_bytes: 30 * 2 ** 30, download_limit: 10000 }], download_price_micros: 500,
    max_upload_bytes: 2 ** 30, domain_limit: 5, payment_methods: ['manual'], support_email: '', usdt_rate_micros: 1000000,
  };
  submissions = []; existingOrders = []; existingLedger = []; createdOrder = undefined; paymentChecks = 0;
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
      if (body.method === 'usdt') createdOrder.usdt_units = Math.round(Number(body.amount) * 1e6) - 990000;
      return json(createdOrder);
    }
    if (path === '/api/orders') return json({ data: existingOrders, total: existingOrders.length });
    if (path === '/api/ledger') return json({ data: existingLedger, total: existingLedger.length });
    if (path === `/api/orders/${order.id}/check`) {
      paymentChecks++;
      createdOrder = { ...(createdOrder || existingOrders.find(o => o.id === order.id) || order), status: 'paid' };
      return json(createdOrder);
    }
    if (path === `/api/orders/${order.id}`) return json(createdOrder || existingOrders.find(o => o.id === order.id) || order);
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
  it('shows two paid rates, three free features, and promotional balance separate from cash', async () => {
    Object.assign(config,{metered_billing:true,download_price_micros:50000,pwa_install_price_micros:50000,cloak_uv_price_micros:1000,return_flow_price_micros:1000,pwa_push_price_micros:1000,registration_gift_limit:0,registration_gift_micros:100e6});
    account.gift_balance_micros=100e6;account.gift_held_micros=50000;
    config.plans=[{...free,name:'按量计费'}];
    openBilling();await screen.findByRole('heading',{name:'用量与账单'});
    const rates=await screen.findByRole('region',{name:'按量计费'});
    expect(within(rates).getAllByText('$0.05')).toHaveLength(2);expect(within(rates).getAllByText('免费',{exact:true})).toHaveLength(3);expect(within(rates).getByText('注册即赠 $100 用量额度。')).toBeTruthy();expect(screen.getByRole('heading',{name:'赠送用量额度'})).toBeTruthy();expect(screen.getByText('$ 99.95')).toBeTruthy();expect(screen.queryByText('注册共享赠送')).toBeNull();
    expect(screen.queryByText('选择适合你的套餐')).toBeNull();expect(screen.queryByRole('button',{name:/开通|续费/})).toBeNull();expect(screen.getByRole('button',{name:'充值余额'})).toBeTruthy();
  });
  it('retains historical Alipay payment instructions without offering new Alipay orders', async () => {
    existingOrders = [{ ...order, method: 'alipay', amount_micros: 100000000, bonus_micros: 0,
      payment_currency: 'CNY', alipay_rate_micros: 6650000, fee_micros: 5000000, pay_cny_cents: 69825,
      pay_url: 'https://payments.example/submit.php?order=test-order' }];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: /查看详情/ }));
    await screen.findByRole('heading', { name: '充值订单' });
    expect(screen.getByText('¥ 698.25')).toBeTruthy();
    expect(screen.getByText(/充值本金 \$100.00 · 手续费 5%（\$5.00）/)).toBeTruthy();
    expect(screen.getByRole('link', { name: '前往支付宝付款' }).getAttribute('href')).toBe('https://payments.example/submit.php?order=test-order');
    expect(screen.getByText(/订单汇率：1 USDT = ¥6.65/)).toBeTruthy();
    expect(submissions).toEqual([]);
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

  it('labels withdrawal holds and refunds and preserves their full amount precision', async () => {
    existingLedger = [
      { id: 'withdrawal-hold', kind: 'withdrawal_hold', amount_micros: -100000001, balance_micros: 1, note: '邀请返利提现申请 · 等待人工审核', reference: 'withdrawal-hold:withdrawal-test', created_at: '2026-10-24T03:00:00Z' },
      { id: 'withdrawal-refund', kind: 'withdrawal_refund', amount_micros: 100000001, balance_micros: 100000002, note: '邀请返利提现退回 · 地址需要核对', reference: 'withdrawal-refund:withdrawal-test', created_at: '2026-10-24T04:00:00Z' },
    ];
    const user = openBilling(); await user.click(await screen.findByRole('button', { name: '余额流水' }));
    expect(await screen.findByText('提现冻结')).toBeTruthy(); expect(screen.getByText('提现退回')).toBeTruthy();
    expect(screen.getByText('− $100.000001')).toBeTruthy(); expect(screen.getByText('+ $100.000001')).toBeTruthy();
    expect(screen.getByText('$0.000001')).toBeTruthy(); expect(screen.getByText('$100.000002')).toBeTruthy();
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
    expect(within(paidCard).getByText('自动检查域名安全')).toBeTruthy();
    expect(within(freeCard).queryByText('自动检查域名安全')).toBeNull();
    const recharge = screen.getByRole('button', { name: '充值余额' });
    expect(recharge.closest('section')?.textContent).toContain('可用余额');
    await user.click(recharge);
    expect(screen.getByRole('heading', { name: '充值账户余额' })).toBeTruthy();
  });

  it('offers only USDT and manual even when cached config advertises retired channels', async () => {
    config.payment_methods = ['alipay', 'wechat', 'manual'];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const channels = within(screen.getByRole('group', { name: '支付方式' }));
    expect(channels.getAllByRole('button')).toHaveLength(2);
    expect((channels.getByRole('button', { name: 'USDT' }) as HTMLButtonElement).disabled).toBe(true);
    expect(channels.getByRole('button', { name: '人工充值' }).getAttribute('aria-pressed')).toBe('true');
    expect(channels.queryByRole('button', { name: '支付宝' })).toBeNull();
    expect(screen.queryByLabelText('支付宝付款预估')).toBeNull();
    expect(channels.queryByText(/微信/)).toBeNull();
  });

  it('shows distinct decorative payment icons and preserves channel switching', async () => {
    config.payment_methods = ['usdt', 'manual'];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const channels = within(screen.getByRole('group', { name: '支付方式' }));
    const usdt = channels.getByRole('button', { name: 'USDT', exact: true });
    const manual = channels.getByRole('button', { name: '人工充值', exact: true });
    expect(usdt.querySelector('.payment-option-icon.usdt[aria-hidden="true"] svg.lucide-coins')).toBeTruthy();
    expect(manual.querySelector('.payment-option-icon.manual[aria-hidden="true"] svg.lucide-headset')).toBeTruthy();
    expect(channels.queryByRole('img')).toBeNull();
    await user.click(manual);
    expect(manual.getAttribute('aria-pressed')).toBe('true');
    expect(usdt.getAttribute('aria-pressed')).toBe('false');
    await user.click(usdt);
    expect(usdt.getAttribute('aria-pressed')).toBe('true');
    expect(submissions).toEqual([]);
  });

  it('submits USDT principal only and shows exact on-chain payment instructions', async () => {
    config.payment_methods = ['usdt', 'manual'];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: '充值余额' }));
    const channels = within(screen.getByRole('group', { name: '支付方式' }));
    expect(channels.getAllByRole('button')).toHaveLength(2);
    expect(channels.queryByRole('button', { name: '支付宝' })).toBeNull();
    expect(channels.getByRole('button', { name: 'USDT' }).getAttribute('aria-pressed')).toBe('true');
    await user.click(screen.getByRole('button', { name: /^\$1000/ }));
    await user.click(screen.getByRole('button', { name: '创建充值订单' }));
    await waitFor(() => expect(submissions).toEqual([{ amount: '1000', method: 'usdt' }]));
    await screen.findByRole('heading', { name: '充值订单' });
    const payment = within(screen.getByRole('dialog'));
    expect(payment.queryByText(/TRON|TRC20/)).toBeNull();
    expect(screen.getByRole('dialog').querySelector('svg[role="img"]')).toBeNull();
    expect(payment.getByText('999.01')).toBeTruthy();
    expect(payment.getByText('TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t')).toBeTruthy();
    expect(payment.getByText('应付金额')).toBeTruthy();
    expect(payment.queryByText('实付金额')).toBeNull();
    expect(payment.queryByText('$ 1,000.00')).toBeNull();
    expect(payment.queryByText('预计到账余额')).toBeNull();
    for (const label of ['订单编号', '支付方式', '有效期至']) expect(payment.queryByText(label)).toBeNull();
    const instructions = screen.getByRole('region', { name: 'USDT 付款信息' });
    expect(instructions.querySelectorAll('.payment-amount')).toHaveLength(1);
    expect(instructions.querySelector('.payment-amount')?.nextElementSibling?.classList.contains('link-card')).toBe(true);
  });

  it('shows only the exact USDT payable amount, copies the amount/address and still checks payment', async () => {
    const wallet = 'TExampleReceivingWallet';
    existingOrders = [{ ...order, method: 'usdt', amount_micros: 50000000, bonus_micros: 0, usdt_units: 49990000, wallet }];
    const user = openBilling();
    const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
    await user.click(await screen.findByRole('button', { name: /查看详情/ }));
    const payment = within(screen.getByRole('dialog'));
    expect(payment.getByText('49.99')).toBeTruthy();
    expect(payment.queryByText('$ 50.00')).toBeNull();
    expect(payment.queryByText(/请准确转入以下金额/)).toBeNull();
    expect(payment.queryByText(order.id)).toBeNull();
    await user.click(payment.getByRole('button', { name: '复制金额' }));
    expect(copy).toHaveBeenLastCalledWith('49.99');
    await user.click(payment.getByRole('button', { name: '复制地址' }));
    expect(copy).toHaveBeenLastCalledWith(wallet);
    await user.click(payment.getByRole('button', { name: '检查到账状态' }));
    await payment.findByRole('heading', { name: '充值已到账' });
    expect(paymentChecks).toBe(1);
    expect(payment.queryByText('收款地址')).toBeNull();
    expect(payment.queryByRole('button', { name: '检查到账状态' })).toBeNull();
    expect(submissions).toEqual([]);
  });

  it('keeps expired-order warnings without exposing order metadata or losing legacy precision', async () => {
    existingOrders = [{ ...order, method: 'usdt', amount_micros: 500000000, bonus_micros: 0, usdt_units: 500009314, wallet: 'TLegacyReceivingWallet', expires_at: '2000-01-01T00:00:00Z' }];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: /查看详情/ }));
    const payment = within(screen.getByRole('dialog'));
    expect(payment.getByText('500.009314')).toBeTruthy();
    expect(payment.getByText('已超时')).toBeTruthy();
    expect(payment.getByText(/订单已超时，请勿继续转账/)).toBeTruthy();
    for (const label of ['实付金额', '订单编号', '支付方式', '有效期至']) expect(payment.queryByText(label)).toBeNull();
  });

  it('retains the promised gift on the USDT paid receipt without showing another payment address', async () => {
    existingOrders = [{ ...order, method: 'usdt', status: 'paid', usdt_units: 999010000, wallet: 'TExampleReceivingWallet' }];
    const user = openBilling();
    await user.click(await screen.findByRole('button', { name: /查看详情/ }));
    const payment = within(screen.getByRole('dialog'));
    expect(payment.getByText('999.01')).toBeTruthy();
    expect(payment.getByText('到账余额')).toBeTruthy();
    expect(payment.getByText('+ $500.00')).toBeTruthy();
    expect(payment.getByText('$1,500.00')).toBeTruthy();
    expect(payment.queryByText('收款地址')).toBeNull();
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
    expect(payment.getByText('应付金额')).toBeTruthy();
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
