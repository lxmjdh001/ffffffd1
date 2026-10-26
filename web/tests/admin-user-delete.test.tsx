import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient } from '@tanstack/react-query';
import { AdminContext, Notification } from 'react-admin';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AdminListPage } from '../src/admin';
import { authProvider } from '../src/api';
import type { User } from '../src/types';

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } });
let users: User[];
let client: QueryClient;
let submissions: Array<{ method: string; body: unknown; csrf: string | null }>;
let deleteGate: Promise<void>;
let rejection: string;
const dialogMethods = {
  showModal: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'showModal'),
  close: Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype, 'close'),
};

beforeEach(() => {
  users = [
    { id: 'member', name: '测试客户', email: 'member@example.com', role: 'member', disabled: false, balance_micros: 0, held_micros: 0, pro_until: null, created_at: '2026-10-26T00:00:00Z' },
    { id: 'admin', name: '平台管理员', email: 'admin@example.com', role: 'admin', disabled: false, balance_micros: 0, held_micros: 0, pro_until: null, created_at: '2026-10-26T00:00:00Z' },
  ];
  submissions = []; rejection = ''; deleteGate = Promise.resolve();
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  vi.stubGlobal('scrollTo', vi.fn());
  Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value(this: HTMLDialogElement) { this.setAttribute('open', ''); } });
  Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value(this: HTMLDialogElement) { this.removeAttribute('open'); } });
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
    const path = String(input).split('?')[0];
    if (path === '/api/me') return json({ user: users.find(u => u.id === 'admin'), csrf_token: 'delete-csrf' });
    if (path === '/api/admin/users') return json({ data: users, total: users.length });
    if (path === '/api/admin/users/member') {
      submissions.push({ method: options?.method || '', body: JSON.parse(String(options?.body)), csrf: new Headers(options?.headers).get('X-CSRF-Token') });
      await deleteGate;
      if (rejection) return json({ error: rejection }, 409);
      users = users.filter(u => u.id !== 'member');
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
  render(<AdminContext authProvider={authProvider} queryClient={client}><AdminListPage resource="users" /><Notification /></AdminContext>);
  await user.click(await screen.findByRole('button', { name: '删除用户' }));
  const dialog = screen.getByRole('dialog');
  return { user, dialog, email: within(dialog).getByRole('textbox', { name: '输入用户邮箱确认删除' }), confirm: within(dialog).getByRole('button', { name: '删除用户' }) as HTMLButtonElement };
}

describe('administrator user deletion', () => {
  it('only offers deletion for members, requires the exact email, and allows cancellation', async () => {
    const { user, dialog, email, confirm } = await openConfirmation();
    const adminRow = screen.getByText('平台管理员', { selector: 'strong' }).closest('tr')!;
    expect(within(adminRow).queryByRole('button', { name: '删除用户' })).toBeNull();
    expect(within(dialog).getByText(/订单、资金流水和审计记录保留/)).toBeTruthy();
    expect(confirm.disabled).toBe(true);
    await user.type(email, 'wrong@example.com');
    expect(confirm.disabled).toBe(true);
    await user.clear(email);
    await user.type(email, 'MEMBER@example.com');
    expect(confirm.disabled).toBe(false);
    await user.click(within(dialog).getByRole('button', { name: '取消' }));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(submissions).toHaveLength(0);
  });

  it('sends a protected DELETE, prevents duplicate clicks and refreshes the list', async () => {
    let finish!: () => void;
    deleteGate = new Promise<void>(resolve => { finish = resolve; });
    const { user, dialog, email, confirm } = await openConfirmation();
    await user.type(email, 'member@example.com');
    await user.click(confirm);
    await waitFor(() => expect(submissions).toEqual([{ method: 'DELETE', body: { confirm_email: 'member@example.com' }, csrf: 'delete-csrf' }]));
    expect(confirm.disabled).toBe(true);
    expect((email as HTMLInputElement).disabled).toBe(true);
    expect((within(dialog).getByRole('button', { name: '取消' }) as HTMLButtonElement).disabled).toBe(true);
    await user.click(confirm);
    expect(submissions).toHaveLength(1);
    finish();
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    await waitFor(() => expect(screen.queryByText('member@example.com')).toBeNull());
    expect(await screen.findByText('用户已删除')).toBeTruthy();
  });

  it('keeps the account and confirmation visible when the server rejects deletion', async () => {
    rejection = '账户仍有余额或冻结用量，请处理完成后再删除';
    const { user, dialog, email, confirm } = await openConfirmation();
    await user.type(email, 'member@example.com');
    await user.click(confirm);
    expect(await within(dialog).findByText(rejection)).toBeTruthy();
    expect(confirm.disabled).toBe(false);
    expect(screen.getByText('测试客户', { selector: 'td > strong' })).toBeTruthy();
    expect(users).toHaveLength(2);
  });
});
