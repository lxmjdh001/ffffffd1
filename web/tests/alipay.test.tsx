import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { alipayQuote, AlipayOrderDetails, AlipaySettings } from '../src/alipay';
import type { Order, SettingsResponse } from '../src/types';

afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

it('quotes CNY with an exact 5% fee and rounds up only the final cent', () => {
 expect(alipayQuote(100000000, 6650000)).toEqual({ feeMicros: 5000000, payCNYCents: 69825 });
 expect(alipayQuote(1000000, 6650000)).toEqual({ feeMicros: 50000, payCNYCents: 699 });
 expect(alipayQuote(1010000, 6650000)).toEqual({ feeMicros: 50500, payCNYCents: 706 });
 expect(alipayQuote(10000000000, 1000000000)).toEqual({ feeMicros: 500000000, payCNYCents: 1050000000 });
 expect(alipayQuote(0, 6650000)).toBeUndefined();
 expect(alipayQuote(1000001, 6650000)).toBeUndefined();
 expect(alipayQuote(100000000, 0)).toBeUndefined();
});

it('saves only the administrator CNY rate in an isolated settings section', async () => {
 const data = { settings: { alipay_cny_rate_micros: 6650000 }, runtime: { futoon_configured: true } } as unknown as SettingsResponse;
 const saved = vi.fn();
 const fetch = vi.fn(async () => new Response(JSON.stringify({ ...data, settings: { alipay_cny_rate_micros: 7250000 } }), { status: 200 }));
 vi.stubGlobal('fetch', fetch);
 render(<AlipaySettings data={data} saved={saved} />);
 expect(screen.getByText('¥698.25')).toBeTruthy();
 const user = userEvent.setup();
 const rate = screen.getByRole('spinbutton', { name: /支付宝汇率/ });
 await user.clear(rate); await user.type(rate, '7.25');
 expect(screen.getByText('¥761.25')).toBeTruthy();
 await user.click(screen.getByRole('button', { name: '保存支付宝配置' }));
 await waitFor(() => expect(saved).toHaveBeenCalledOnce());
 const [path, options] = fetch.mock.calls[0] as unknown as [string, RequestInit];
 expect(path).toBe('/api/admin/settings');
 expect(options.method).toBe('PUT');
 expect(JSON.parse(String(options.body))).toEqual({ section: 'alipay', settings: { alipay_cny_rate_micros: 7250000 } });
 expect(screen.getByRole('status').textContent).toBe('已保存');
});

it('rejects an invalid exchange rate locally without sending a settings change', async () => {
 const fetch = vi.fn(); vi.stubGlobal('fetch', fetch);
 render(<AlipaySettings data={{ settings: {}, runtime: {} } as SettingsResponse} saved={vi.fn()} />);
 const user = userEvent.setup();
 const rate = screen.getByRole('spinbutton', { name: /支付宝汇率/ });
 await user.clear(rate); await user.type(rate, '0');
 await user.click(screen.getByRole('button', { name: '保存支付宝配置' }));
 expect(screen.getByRole('alert').textContent).toContain('汇率须为');
 expect(fetch).not.toHaveBeenCalled();
});

it('retains the locked order quote and blocks unsafe or expired gateway links', () => {
 const order = { method: 'alipay', payment_currency: 'CNY', status: 'pending', amount_micros: 100000000, fee_micros: 5000000, alipay_rate_micros: 6650000, pay_cny_cents: 69825, pay_url: 'https://gateway.example/submit.php?sign=test' } as Order;
 const view = render(<AlipayOrderDetails order={order} expired={false} />);
 expect(screen.getByText(/订单汇率：1 USDT = ¥6.65/)).toBeTruthy();
 const link = screen.getByRole('link', { name: '前往支付宝付款' });
 expect(link.getAttribute('href')).toBe(order.pay_url); expect(link.getAttribute('rel')).toContain('noopener');
 view.rerender(<AlipayOrderDetails order={order} expired />);
 expect(screen.queryByRole('link')).toBeNull();
 view.rerender(<AlipayOrderDetails order={{ ...order, pay_url: 'javascript:alert(1)' }} expired={false} />);
 expect(screen.queryByRole('link')).toBeNull();
 expect(screen.getByRole('alert').textContent).toContain('付款链接不可用');
});
