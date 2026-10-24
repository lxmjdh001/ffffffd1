import { cleanup, render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, expect, it, vi } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { UsdtPaymentDetails } from '../src/usdt-payment-details';
import type { Order } from '../src/types';

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it('displays and copies a two-decimal amount and address without a QR code or channel badge', async () => {
  const user = userEvent.setup();
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
  const wallet = 'TExampleReceivingWallet';
  const { container } = render(<UsdtPaymentDetails order={{ usdt_units: 499010000, wallet } as Order} />);
  expect(screen.getByText('499.01')).toBeTruthy();
  expect(screen.getByText('应付金额')).toBeTruthy();
  expect(container.querySelectorAll('.payment-amount')).toHaveLength(1);
  expect(container.querySelector('.payment-amount')?.nextElementSibling?.classList.contains('link-card')).toBe(true);
  expect(container.querySelector('svg:not(.lucide)')).toBeNull();
  expect(container.querySelector('.badge')).toBeNull();
  expect(screen.queryByText(/TRON|TRC20|二维码/)).toBeNull();
  expect(screen.getByText('收款地址')).toBeTruthy();
  await user.click(screen.getByRole('button', { name: '复制金额' }));
  expect(copy).toHaveBeenLastCalledWith('499.01');
  await user.click(screen.getByRole('button', { name: '复制地址' }));
  expect(copy).toHaveBeenLastCalledWith(wallet);
});

it('keeps the exact amount and copy precision of historical six-decimal orders', async () => {
  const user = userEvent.setup();
  const copy = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue();
  render(<UsdtPaymentDetails order={{ usdt_units: 500009314, wallet: 'TExampleReceivingWallet' } as Order} />);
  expect(screen.getByText('500.009314')).toBeTruthy();
  await user.click(screen.getByRole('button', { name: '复制金额' }));
  expect(copy).toHaveBeenCalledWith('500.009314');
});

it('shows trailing cents for whole-USDT payments', () => {
  render(<UsdtPaymentDetails order={{ usdt_units: 500000000, wallet: 'TExampleReceivingWallet' } as Order} />);
  expect(screen.getByText('500.00')).toBeTruthy();
});

it('uses compact two-column payment choices on desktop and one column on mobile', () => {
  const style = document.createElement('style');
  style.textContent = readFileSync(resolve(process.cwd(), 'src/styles.css'), 'utf8');
  document.head.append(style);
  try {
    const rules = Array.from(style.sheet!.cssRules);
    const grid = rules.find(rule => rule.type === 1 && (rule as CSSStyleRule).selectorText === '.payment-options') as CSSStyleRule;
    const button = rules.find(rule => rule.type === 1 && (rule as CSSStyleRule).selectorText === '.payment-options button') as CSSStyleRule;
    expect(grid.style.getPropertyValue('grid-template-columns')).toBe('repeat(2,minmax(0,1fr))');
    expect(button.style.getPropertyValue('min-height')).toBe('44px');
    expect(rules.some(rule => rule.type === 4 && (rule as CSSMediaRule).conditionText.replace(/\s/g, '') === '(max-width:650px)' &&
      Array.from((rule as CSSMediaRule).cssRules).some(inner => inner.type === 1 && (inner as CSSStyleRule).selectorText === '.payment-options' && (inner as CSSStyleRule).style.getPropertyValue('grid-template-columns') === '1fr'))).toBe(true);
  } finally { style.remove(); }
});
