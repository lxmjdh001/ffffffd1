import type { ReactNode } from 'react';
import type { Order } from './types';
import { CopyButton, money } from './ui';
import './usdt-payment-details.css';

export function UsdtPaymentDetails({ order, status, showAddress = true }: { order: Order; status?: ReactNode; showAddress?: boolean }) {
  // Existing orders retain their exact six-decimal amount; never round a
  // previously issued payment instruction to the new cent precision.
  const digits = order.usdt_units % 10000 === 0 ? 2 : 6;
  return <section className="usdt-payment-instructions" aria-label="USDT 付款信息">
    <div className="payment-amount usdt-payment-amount">
      <span>应付金额</span>
      <strong className="usdt-amount">{money(order.usdt_units, digits)} <small>USDT</small></strong>
      <CopyButton value={(order.usdt_units / 1e6).toFixed(digits)} label="复制金额" />
      {status && <div className="usdt-payment-status">{status}</div>}
    </div>
    {showAddress && <>
    <div className="link-card">
      <small>收款地址</small>
      <code className="break">{order.wallet}</code>
      <CopyButton value={order.wallet} label="复制地址" />
    </div>
    <div className="notice compact">请核对转账网络与收款地址匹配，实际到账金额须与订单一致。转账手续费由发送方另付。</div>
    </>}
  </section>;
}
