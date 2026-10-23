import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { UsdtWalletSettings } from '../src/UsdtWalletSettings';
import type { SettingsResponse } from '../src/types';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
const wallet = 'TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t';
const data = { settings: { usdt_wallet: null }, runtime: { usdt_wallet: '', usdt_provider: 'uzf', usdt_address_password_configured: true } } as unknown as SettingsResponse;

it('requires a secondary password, sends it only to the dedicated endpoint and clears it after success', async () => {
  const saved = vi.fn();
  const fetch = vi.fn(async () => new Response(JSON.stringify({ ...data, runtime: { ...data.runtime, usdt_wallet: wallet } }), { headers: { 'Content-Type': 'application/json' } }));
  vi.stubGlobal('fetch', fetch);
  render(<UsdtWalletSettings data={data} saved={saved} />);
  const user = userEvent.setup();
  const address = screen.getByLabelText('USDT 收款地址（TRC20）');
  const password = screen.getByLabelText('收款地址二级密码') as HTMLInputElement;
  await user.type(address, wallet);
  await user.click(screen.getByRole('button', { name: '验证二级密码并保存地址' }));
  expect(fetch).not.toHaveBeenCalled();
  await user.type(password, 'test-secondary-password');
  await user.click(screen.getByRole('button', { name: '验证二级密码并保存地址' }));
  await waitFor(() => expect(saved).toHaveBeenCalledOnce());
  const [path, options] = fetch.mock.calls[0] as unknown as [string, RequestInit];
  expect(path).toBe('/api/admin/usdt-wallet');
  expect(options.method).toBe('PUT');
  expect(JSON.parse(String(options.body))).toEqual({ wallet, secondary_password: 'test-secondary-password' });
  expect(password.value).toBe('');
});

it('does not report an address as saved when secondary verification is rejected', async () => {
  const saved = vi.fn();
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({ error: '二级密码错误，收款地址未修改' }), { status: 403, headers: { 'Content-Type': 'application/json' } })));
  render(<UsdtWalletSettings data={data} saved={saved} />);
  const user = userEvent.setup();
  await user.type(screen.getByLabelText('收款地址二级密码'), 'wrong');
  await user.click(screen.getByRole('button', { name: '验证二级密码并保存地址' }));
  expect(await screen.findByText('二级密码错误，收款地址未修改')).toBeTruthy();
  expect(saved).not.toHaveBeenCalled();
  expect((screen.getByLabelText('收款地址二级密码') as HTMLInputElement).value).toBe('');
});
