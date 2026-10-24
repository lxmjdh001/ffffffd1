import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AdminReferralWithdrawalsPage, ReferralWithdrawals, withdrawalAmountMicros } from '../src/referral-withdrawals';
import { ReferralsPage } from '../src/referrals';
import type { Referrals, ReferralWithdrawal } from '../src/types';

const permission = vi.hoisted(() => ({ role: 'admin', pending: false }));
vi.mock('react-admin', () => ({ usePermissions: () => ({ permissions: permission.role, isPending: permission.pending }), useNotify: () => vi.fn() }));

const address = 'TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE';
const summary: Referrals = { invite_code: 'aabbcc', invite_url: 'https://console.example/#/register?invite=aabbcc', rates: [10, 2, 2], total_reward_micros: 700_000_000, withdrawal_available_micros: 150_000_001, withdrawal_pending_micros: 100_000_000, withdrawal_paid_micros: 200_000_000, withdrawal_minimum_micros: 100_000_000, team_count: 8, direct_count: 3, level_counts: [3, 3, 2], rewards: { data: [], total: 0 }, members: { data: [], total: 0 } };
const record: ReferralWithdrawal = { id: 'withdrawal-one', user_id: 'member-one', user_name: '提现申请人', user_email: 'member@example.invalid', amount_micros: 100_000_000, address, status: 'pending', review_note: '', payout_reference: null, created_at: '2026-10-24T02:00:00Z', reviewed_at: null, paid_at: null };
const dialogDescriptors = { showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'), close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close') };
let client: QueryClient;
let writes: { path: string; body: Record<string, unknown>; headers: Headers }[];
let requests: URL[];
let current: ReferralWithdrawal;
let failWrite: boolean;
let deferWrite: Promise<void> | undefined;
let userRows: ReferralWithdrawal[];

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } });
  writes = []; requests = []; current = structuredClone(record); userRows = []; failWrite = false; deferWrite = undefined; permission.role = 'admin'; permission.pending = false;
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const url = new URL(String(input), 'https://console.example'); requests.push(url);
    if (options.method === 'POST') {
      const body = JSON.parse(String(options.body)); writes.push({ path: url.pathname, body, headers: new Headers(options.headers) });
      if (deferWrite) await deferWrite;
      if (failWrite) return new Response(JSON.stringify({ error: '网络暂时异常，请重试' }), { status: 503 });
      if (url.pathname === '/api/referral-withdrawals') { userRows = [{ ...current, amount_micros: withdrawalAmountMicros(String(body.amount))! }]; return new Response(JSON.stringify(userRows[0])); }
      if (url.pathname.endsWith('/review')) { current = { ...current, status: body.action === 'approve' ? 'approved' : body.action === 'paid' ? 'paid' : 'rejected', review_note: body.note, payout_reference: body.transaction_id || null, reviewed_at: '2026-10-24T03:00:00Z', paid_at: body.action === 'paid' ? '2026-10-24T04:00:00Z' : null }; return new Response(JSON.stringify(current)); }
    }
    if (url.pathname === '/api/referrals') return new Response(JSON.stringify(summary));
    if (url.pathname === '/api/referral-withdrawals') return new Response(JSON.stringify({ data: userRows, total: userRows.length }));
    if (url.pathname === '/api/admin/referral-withdrawals') return new Response(JSON.stringify({ data: [current], total: 1 }));
    throw new Error(`Unexpected request: ${input}`);
  }));
});

afterEach(() => {
  cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks();
  for (const [key, descriptor] of Object.entries(dialogDescriptors)) {
    if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor); else Reflect.deleteProperty(HTMLDialogElement.prototype, key);
  }
});

function renderUser(data = summary) {
  const close = vi.fn();
  render(<QueryClientProvider client={client}><ReferralWithdrawals summary={data} open close={close} /></QueryClientProvider>);
  return { user: userEvent.setup(), close };
}
function renderAdmin() { render(<QueryClientProvider client={client}><AdminReferralWithdrawalsPage /></QueryClientProvider>); return userEvent.setup(); }

describe('referral withdrawals', () => {
  it('pairs each summary label with its own amount without flex notice line breaks', () => {
    renderUser();
    const dialog = screen.getByRole('dialog');
    const summary = dialog.querySelector('.withdrawal-request-summary')!;
    const entries = summary.querySelectorAll(':scope > div');
    expect(entries).toHaveLength(2);
    expect(entries[0].querySelector('dt')?.textContent).toBe('可提现额度：');
    expect(entries[0].querySelector('dd')?.textContent).toBe('$ 150.000001');
    expect(entries[1].querySelector('dt')?.textContent).toBe('单笔最低提现：');
    expect(entries[1].querySelector('dd')?.textContent).toBe('$ 100.00');
    expect(summary.querySelector('br')).toBeNull();
    expect(summary.classList.contains('notice')).toBe(false);
  });

  it('uses available reward balance rather than lifetime total and rejects insufficient or over-precise amounts', async () => {
    const { user } = renderUser();
    const form = within(screen.getByRole('dialog'));
    await user.type(form.getByLabelText('USDT（TRC20）收款地址'), address);
    const input = form.getByLabelText('提现金额（美元）');
    const button = form.getByRole('button', { name: '提交提现申请' }) as HTMLButtonElement;
    for (const invalid of ['99.999999', '150.000002', '100.0000001', '1e2', '-100']) {
      await user.clear(input); await user.type(input, invalid); expect(button.disabled).toBe(true);
    }
    await user.clear(input); await user.type(input, '150.000001'); expect(button.disabled).toBe(false);
    expect(within(screen.getByRole('region', { name: '返利提现' })).getAllByText('$ 150.000001')).toHaveLength(2);
    expect(writes).toHaveLength(0);
  });

  it('keeps a stable request key on retry and sends the precise USD string with a TRC20 address', async () => {
    const { user, close } = renderUser(); const form = within(screen.getByRole('dialog'));
    await user.type(form.getByLabelText('提现金额（美元）'), '100.000001');
    await user.type(form.getByLabelText('USDT（TRC20）收款地址'), address);
    failWrite = true; await user.click(form.getByRole('button', { name: '提交提现申请' }));
    expect(await form.findByRole('alert')).toBeTruthy(); expect(close).not.toHaveBeenCalled();
    failWrite = false; await user.click(form.getByRole('button', { name: '提交提现申请' }));
    await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
    expect(writes).toHaveLength(2); expect(writes[0].body).toEqual({ amount: '100.000001', address });
    const firstKey = writes[0].headers.get('Idempotency-Key'); expect(firstKey).toBeTruthy(); expect(writes[1].headers.get('Idempotency-Key')).toBe(firstKey);
    expect(await screen.findByText('提现申请已提交，请等待管理员审核。')).toBeTruthy();
  });

  it('prevents repeated submits and closing while the submission is in progress', async () => {
    let release!: () => void; deferWrite = new Promise<void>(resolve => { release = resolve; });
    const { user, close } = renderUser(); const form = within(screen.getByRole('dialog'));
    await user.type(form.getByLabelText('提现金额（美元）'), '100'); await user.type(form.getByLabelText('USDT（TRC20）收款地址'), address);
    const submit = form.getByRole('button', { name: '提交提现申请' }); await user.dblClick(submit);
    expect(writes).toHaveLength(1); expect((form.getByRole('button', { name: '提交中…' }) as HTMLButtonElement).disabled).toBe(true);
    await user.click(form.getByRole('button', { name: '关闭' })); expect(close).not.toHaveBeenCalled();
    release(); await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
  });

  it('retains the retry key when only insignificant decimal zeroes change', async () => {
    const { user, close } = renderUser(); const form = within(screen.getByRole('dialog'));
    await user.type(form.getByLabelText('提现金额（美元）'), '100.0'); await user.type(form.getByLabelText('USDT（TRC20）收款地址'), address);
    failWrite = true; await user.click(form.getByRole('button', { name: '提交提现申请' })); await form.findByRole('alert');
    await user.clear(form.getByLabelText('提现金额（美元）')); await user.type(form.getByLabelText('提现金额（美元）'), '100.000000');
    failWrite = false; await user.click(form.getByRole('button', { name: '提交提现申请' })); await waitFor(() => expect(close).toHaveBeenCalledTimes(1));
    expect(writes).toHaveLength(2); expect(writes[0].headers.get('Idempotency-Key')).toBe(writes[1].headers.get('Idempotency-Key'));
  });

  it('shows the threshold even when lifetime earnings exceed $100 and displays customer record details', async () => {
    userRows = [{ ...record, status: 'rejected', review_note: '收款地址需要重新核对', reviewed_at: '2026-10-24T03:00:00Z' }];
    renderUser({ ...summary, withdrawal_available_micros: 99_000_000 });
    expect(screen.getByText('可提现额度尚未达到 $100，请达到门槛后再申请。')).toBeTruthy();
    expect(await screen.findByText('已驳回')).toBeTruthy();
    expect(screen.getByText(/收款地址需要重新核对/)).toBeTruthy();
    expect(screen.getByText(address)).toBeTruthy();
  });

  it('changes the requested team wording and opens withdrawal from the cumulative earnings card', async () => {
    render(<QueryClientProvider client={client}><ReferralsPage /></QueryClientProvider>);
    const user = userEvent.setup(); await screen.findByRole('heading', { name: '邀请好友' });
    expect(screen.getByText('分享邀请链接，享受长期邀请返利。')).toBeTruthy(); expect(screen.getByText('团队下级人数')).toBeTruthy(); expect(screen.getByText('下级邀请好友总数')).toBeTruthy();
    await user.click(screen.getByRole('button', { name: '提现', exact: true })); expect(screen.getByRole('dialog').textContent).toContain('申请提现');
  });

  it('explains manual reconciliation and blocks withdrawal when eligibility needs review', async () => {
    const { user } = renderUser({ ...summary, withdrawal_reconciliation_required: true });
    expect(screen.getByRole('alert').textContent).toContain('提现资格需要人工核对，请联系客服。');
    const form = within(screen.getByRole('dialog')); await user.type(form.getByLabelText('提现金额（美元）'), '100'); await user.type(form.getByLabelText('USDT（TRC20）收款地址'), address);
    expect((form.getByRole('button', { name: '提交提现申请' }) as HTMLButtonElement).disabled).toBe(true); expect(writes).toHaveLength(0);
  });

  it('parses six decimal places without floating point rounding and refuses unsafe numbers', () => {
    expect(withdrawalAmountMicros('100.000001')).toBe(100_000_001); expect(withdrawalAmountMicros('100.100001')).toBe(100_100_001); expect(withdrawalAmountMicros('100.0000001')).toBeUndefined(); expect(withdrawalAmountMicros('9007199254740992')).toBeUndefined();
  });
});

describe('admin withdrawal review', () => {
  it('guards non-admin users before loading any records', async () => {
    permission.role = 'member'; renderAdmin();
    expect(screen.getByText('此页面需要管理员权限')).toBeTruthy();
    await new Promise(resolve => setTimeout(resolve, 0)); expect(requests).toHaveLength(0);
  });

  it('requires a visible rejection reason and shows the result after refund review', async () => {
    const user = renderAdmin(); await user.click(await screen.findByRole('button', { name: '驳回', exact: true }));
    const form = within(screen.getByRole('dialog')); const confirm = form.getByRole('button', { name: '驳回申请' }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true); await user.type(form.getByLabelText('驳回原因'), '地址需要核对'); expect(confirm.disabled).toBe(false); await user.click(confirm);
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(writes[0].body).toEqual({ action: 'reject', note: '地址需要核对' });
    expect(await screen.findAllByText('已驳回')).toHaveLength(2); expect(screen.queryByRole('button', { name: '驳回', exact: true })).toBeNull();
  });

  it('approves first, then registers an offline payout only with a 64-character TRC20 transaction hash', async () => {
    const user = renderAdmin(); await user.click(await screen.findByRole('button', { name: '审核通过', exact: true }));
    let form = within(screen.getByRole('dialog')); await user.click(form.getByRole('button', { name: '审核通过', exact: true }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(writes[0].body).toEqual({ action: 'approve', note: '' });
    await user.click(await screen.findByRole('button', { name: '登记打款' })); form = within(screen.getByRole('dialog'));
    expect(form.getByText(/不会自动转账/)).toBeTruthy(); const confirm = form.getByRole('button', { name: '确认已线下打款' }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true); await user.type(form.getByLabelText('打款交易号'), 'not-a-valid-hash'); expect(confirm.disabled).toBe(true);
    await user.clear(form.getByLabelText('打款交易号')); const transactionID = 'a1'.repeat(32); await user.type(form.getByLabelText('打款交易号'), transactionID);
    await user.type(form.getByLabelText('审核备注（选填）'), '已核对完成转账'); expect(confirm.disabled).toBe(false); await user.click(confirm);
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(writes[1].body).toEqual({ action: 'paid', note: '已核对完成转账', transaction_id: transactionID });
    expect(await screen.findAllByText('已打款')).toHaveLength(2); expect(screen.getByText(transactionID)).toBeTruthy(); expect(screen.queryByRole('button', { name: '登记打款' })).toBeNull();
  });

  it('sends status and customer search filters to the server', async () => {
    const user = renderAdmin(); await screen.findByText('提现申请人'); await user.selectOptions(screen.getByLabelText('提现状态筛选'), 'approved');
    await user.type(screen.getByLabelText('搜索提现记录'), 'member@example.invalid{Enter}');
    await waitFor(() => expect(requests.some(url => url.searchParams.get('status') === 'approved' && url.searchParams.get('q') === 'member@example.invalid')).toBe(true));
  });
});
