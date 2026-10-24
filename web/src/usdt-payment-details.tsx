import type { Order } from './types';
import { CopyButton, money } from './ui';

export function UsdtPaymentDetails({ order }: { order: Order }) {
  // Existing orders retain their exact six-decimal amount; never round a
  // previously issued payment instruction to the new cent precision.
  const digits = order.usdt_units % 10000 === 0 ? 2 : 6;
  return <>
    <div className="usdt-pay"><div>
      <p>请准确转入以下金额</p>
      <strong className="usdt-amount">{money(order.usdt_units, digits)} <small>USDT</small></strong>
      <CopyButton value={(order.usdt_units / 1e6).toFixed(digits)} label="复制金额" />
    </div></div>
    <div className="link-card">
      <small>收款地址</small>
      <code className="break">{order.wallet}</code>
      <CopyButton value={order.wallet} label="复制地址" />
    </div>
    <div className="notice compact">请核对转账网络与收款地址匹配，实际到账金额须与订单一致。转账手续费由发送方另付。</div>
  </>;
}
