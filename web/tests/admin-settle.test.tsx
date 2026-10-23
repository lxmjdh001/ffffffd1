import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AdminListPage } from '../src/admin';
import { authProvider } from '../src/api';
import type { Order } from '../src/types';

const json = (body: unknown) => new Response(JSON.stringify(body), { headers: { 'Content-Type': 'application/json' } });
let order: Order;
let submissions: Array<{ trade_id: string; note: string }>;
let confirmGate: Promise<void>;
let client: QueryClient;
const dialogMethods = {
  showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'),
  close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close'),
};

beforeEach(() => {
  order = {
    id: 'test-order', user_id: 'test-member', method: 'manual', amount_micros: 100000000, bonus_micros: 0,
    status: 'pending', trade_id: null, pay_url: '', usdt_units: 0, wallet: '', rate_micros: 0,
    expires_at: '2026-10-08T00:00:00Z', created_at: '2026-10-07T00:00:00Z', paid_at: null,
  };
  submissions = [];
  confirmGate = Promise.resolve();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.stubGlobal('scrollTo', vi.fn());
  // jsdom does not implement the native dialog methods; keep their visible
  // open/closed behavior so the real modal and form can be exercised.
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const path = String(input).split('?')[0];
    if (path === '/api/me') return json({ user: { id: 'test-admin', role: 'admin' }, csrf_token: 'test-csrf' });
    if (path === '/api/admin/orders') return json({ data: [order], total: 1 });
    if (path === `/api/admin/orders/${order.id}/settle`) {
      submissions.push(JSON.parse(String(options?.body)));
      await confirmGate;
      order = { ...order, status: 'paid' };
      return json({ ok: true });
    }
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

async function openConfirmation() {
  const user = userEvent.setup();
  render(<AdminContext authProvider={authProvider} queryClient={client}><AdminListPage resource="orders" /></AdminContext>);
  await user.click(await screen.findByRole('button', { name: '确认入账' }));
  const dialog = screen.getByRole('dialog');
  return { user, dialog, confirm: within(dialog).getByRole('button', { name: '确认入账' }) };
}

describe('administrator payment confirmation', () => {
  it('includes the promised gift in the confirmation total', async () => {
    order.amount_micros = 1000000000;
    order.bonus_micros = 300000000;
    const { dialog } = await openConfirmation();
    expect(within(dialog).getByText('$1,300.00')).toBeTruthy();
    expect(within(dialog).getByText('充值 $1,000.00 + 活动赠送 $300.00')).toBeTruthy();
  });

  it.each(['manual', 'usdt'])('confirms a %s order with both optional fields empty and blocks double clicks', async method => {
    order.method = method;
    let finish!: () => void;
    confirmGate = new Promise<void>(resolve => { finish = resolve; });
    const { user, dialog, confirm } = await openConfirmation();
    expect(within(dialog).getByText('$100.00')).toBeTruthy();
    await user.click(confirm);
    await waitFor(() => expect(submissions).toEqual([{ trade_id: '', note: '' }]));
    expect((confirm as HTMLButtonElement).disabled).toBe(true);
    await user.click(confirm);
    expect(submissions).toHaveLength(1);
    finish();
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    await screen.findByText('已完成');
  });

  it('accepts a short transaction reference and a three-character note', async () => {
    const { user, dialog, confirm } = await openConfirmation();
    await user.click(within(dialog).getByText('添加交易号或备注（选填）'));
    await user.type(within(dialog).getByRole('textbox', { name: '交易号（选填）' }), '1');
    await user.type(within(dialog).getByRole('textbox', { name: '备注（选填）' }), '已到账');
    await user.click(confirm);
    await waitFor(() => expect(submissions).toEqual([{ trade_id: '1', note: '已到账' }]));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  });
});
