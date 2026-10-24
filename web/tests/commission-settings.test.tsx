import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { CommissionSettingsPage } from '../src/commission-settings';
import { AdminListPage } from '../src/admin';
import type { CommissionRates, User } from '../src/types';

const permission = vi.hoisted(() => ({ role: 'admin', pending: false }));
vi.mock('react-admin', async () => ({ ...await vi.importActual('react-admin'), usePermissions: () => ({ permissions: permission.role, isPending: permission.pending }), useNotify: () => vi.fn() }));
const profile: User = { id: 'commission-member', name: '佣金测试用户', email: 'commission@example.invalid', role: 'member', disabled: false, balance_micros: 20_000_000, held_micros: 0, pro_until: null, created_at: '2026-10-24T02:00:00Z', direct_invite_count: 4, team_invite_count: 12, total_commission_micros: 123_456_789, referral_rates: [10, 2, 2], referral_rates_overridden: false };
const dialogDescriptors = { showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'), close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close') };
let client: QueryClient;
let rates: CommissionRates;
let current: User;
let writes: { method: string; path: string; body: unknown }[];
let requests: string[];
let gate: Promise<void> | undefined;
let fail: boolean;

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  rates = [10, 2, 2]; current = structuredClone(profile); writes = []; requests = []; gate = undefined; fail = false; permission.role = 'admin'; permission.pending = false;
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options: RequestInit = {}) => {
    const path = String(input).split('?')[0]; requests.push(path);
    const method = options.method || 'GET';
    if (method !== 'GET') {
      const body = options.body ? JSON.parse(String(options.body)) : undefined; writes.push({ method, path, body }); if (gate) await gate;
      if (fail) return new Response(JSON.stringify({ error: '佣金设置保存失败' }), { status: 503 });
      if (path === '/api/admin/commission-settings') { rates = body.rates.map(Number) as CommissionRates; if (!current.referral_rates_overridden) current.referral_rates = rates; return new Response(JSON.stringify({ rates })); }
      if (path === `/api/admin/users/${profile.id}/referral-rates`) { current = { ...current, referral_rates: method === 'DELETE' ? rates : body.rates.map(Number), referral_rates_overridden: method !== 'DELETE' }; return new Response(JSON.stringify({ rates: current.referral_rates, overridden: current.referral_rates_overridden })); }
    }
    if (path === '/api/admin/commission-settings') return new Response(JSON.stringify({ rates }));
    if (path === '/api/admin/users') return new Response(JSON.stringify({ data: [current], total: 1 }));
    throw new Error(`Unexpected request: ${input}`);
  }));
});

afterEach(() => {
  cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks();
  for (const [key, descriptor] of Object.entries(dialogDescriptors)) { if (descriptor) Object.defineProperty(HTMLDialogElement.prototype, key, descriptor); else Reflect.deleteProperty(HTMLDialogElement.prototype, key); }
});

function open(page: 'settings' | 'users' = 'settings') {
  render(<QueryClientProvider client={client}>{page === 'settings' ? <CommissionSettingsPage /> : <AdminListPage resource="users" />}</QueryClientProvider>);
  return userEvent.setup();
}
async function fill(user: ReturnType<typeof userEvent.setup>, root: ReturnType<typeof within>, values: [string, string, string]) {
  for (const [index, label] of ['一级返佣比例（%）', '二级返佣比例（%）', '三级返佣比例（%）'].entries()) { const input = root.getByLabelText(label); await user.clear(input); if (values[index]) await user.type(input, values[index]); }
}

describe('commission settings', () => {
  it('requires administrator access before loading commission settings', async () => {
    permission.role = 'member'; open(); expect(screen.getByText('此页面需要管理员权限')).toBeTruthy(); await new Promise(resolve => setTimeout(resolve, 0)); expect(requests).toHaveLength(0);
  });

  it('loads the default three rates and saves all three as precise percentage strings', async () => {
    const user = open(); await screen.findByLabelText('一级返佣比例（%）');
    expect((screen.getByLabelText('一级返佣比例（%）') as HTMLInputElement).value).toBe('10'); expect((screen.getByLabelText('二级返佣比例（%）') as HTMLInputElement).value).toBe('2');
    expect(screen.getByText(/个人设置优先于全局设置，可高于或低于全局比例/)).toBeTruthy();
    await fill(user, within(document.body), ['0', '100', '0.25']); await user.click(screen.getByRole('button', { name: '保存全局比例' }));
    expect(await screen.findByRole('status')).toBeTruthy(); expect(writes).toEqual([{ method: 'PUT', path: '/api/admin/commission-settings', body: { rates: ['0', '100', '0.25'] } }]);
  });

  it('disables saving incomplete, negative, over-100 and over-precision percentages', async () => {
    const user = open(); await screen.findByLabelText('一级返佣比例（%）'); const input = screen.getByLabelText('一级返佣比例（%）');
    for (const value of ['', '-1', '100.01', '1.001', '1e2']) { await user.clear(input); if (value) await user.type(input, value); expect((screen.getByRole('button', { name: '保存全局比例' }) as HTMLButtonElement).disabled).toBe(true); }
    expect(writes).toHaveLength(0);
  });

  it('prevents duplicate saves and retains the draft after a failed save', async () => {
    let release!: () => void; gate = new Promise<void>(resolve => { release = resolve; }); fail = true;
    const user = open(); await screen.findByLabelText('一级返佣比例（%）'); await fill(user, within(document.body), ['12.5', '3', '2']); await user.dblClick(screen.getByRole('button', { name: '保存全局比例' }));
    expect(writes).toHaveLength(1); expect((screen.getByRole('button', { name: '保存中…' }) as HTMLButtonElement).disabled).toBe(true); release();
    expect(await screen.findByRole('alert')).toBeTruthy(); expect((screen.getByLabelText('一级返佣比例（%）') as HTMLInputElement).value).toBe('12.5');
    fail = false; await user.click(screen.getByRole('button', { name: '保存全局比例' })); await screen.findByRole('status'); expect(writes).toHaveLength(2);
  });
});

describe('per-user commissions', () => {
  it('shows each user’s direct and team invitation counts, cumulative commission and effective rate source', async () => {
    open('users'); const row = (await screen.findByText(profile.name)).closest('tr')!; const table = within(row);
    expect(table.getByText('直接邀请').textContent).toContain('4'); expect(table.getByText('团队人数 12')).toBeTruthy(); expect(table.getByText('$ 123.456789')).toBeTruthy();
    expect(table.getByText('10%')).toBeTruthy(); expect(table.getAllByText('2%')).toHaveLength(2); expect(table.getByText('跟随全局')).toBeTruthy(); expect(table.getByRole('button', { name: '设置佣金' })).toBeTruthy();
  });

  it('sets a complete personal override even when its rates are below the global rates', async () => {
    const user = open('users'); await user.click(await screen.findByRole('button', { name: '设置佣金' })); const form = within(screen.getByRole('dialog'));
    await fill(user, form, ['1', '0', '0.25']); await user.click(form.getByRole('button', { name: '保存个人比例' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull()); expect(writes[0]).toEqual({ method: 'PUT', path: `/api/admin/users/${profile.id}/referral-rates`, body: { rates: ['1', '0', '0.25'] } });
    expect(await screen.findByText('个人设置')).toBeTruthy(); expect(screen.getByText('0.25%')).toBeTruthy();
  });

  it('previews current global rates before an explicit reset and removes the override', async () => {
    current = { ...current, referral_rates: [15, 3, 2], referral_rates_overridden: true }; rates = [12.5, 1.5, 0.5];
    const user = open('users'); await user.click(await screen.findByRole('button', { name: '设置佣金' })); const form = within(screen.getByRole('dialog'));
    await user.click(form.getByRole('button', { name: '恢复全局设置' })); expect(await form.findByText('12.5%')).toBeTruthy(); expect(form.getByText('1.5%')).toBeTruthy(); expect(form.getByText('0.5%')).toBeTruthy(); expect(writes).toHaveLength(0);
    await user.click(form.getByRole('button', { name: '确认恢复全局' })); await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(writes[0]).toEqual({ method: 'DELETE', path: `/api/admin/users/${profile.id}/referral-rates`, body: undefined }); expect(await screen.findByText('跟随全局')).toBeTruthy();
  });

  it('keeps the individual settings dialog and rates available after an API error', async () => {
    const user = open('users'); await user.click(await screen.findByRole('button', { name: '设置佣金' })); const form = within(screen.getByRole('dialog'));
    await fill(user, form, ['20', '4', '3']); fail = true; await user.click(form.getByRole('button', { name: '保存个人比例' }));
    expect(await form.findByRole('alert')).toBeTruthy(); expect((form.getByLabelText('一级返佣比例（%）') as HTMLInputElement).value).toBe('20'); expect(screen.getByRole('dialog')).toBeTruthy();
  });
});
