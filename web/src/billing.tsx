import { UsageRates } from './usage-billing';
import './billing.css';
// i18n: static UI literals
import { localizedPlanName, t, useLocale } from './i18n';
import { useSearchParams } from 'react-router-dom';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { useNotify } from 'react-admin';
import { UsdtPaymentDetails } from './usdt-payment-details';
import { ArrowDownToLine, ArrowRight, ArrowUpRight, CalendarDays, Check, Coins, CreditCard, Boxes, HardDrive, Headset, Plus, RefreshCw, ShieldCheck, Wallet, Zap } from 'lucide-react';
import { api, APIError, newKey, send } from './api';
import { rechargeBonus, rechargeMoney, rechargePercent } from './recharge';
import { AlipayOrderDetails, cnyMoney } from './alipay';
import type { Dashboard, Ledger, Order, Page } from './types';
import { Badge, bytes, CheckLine, date, Empty, ErrorState, errorText, Field, FormError, ledgerKind, Modal, money, number, PageHeader, Pager, Spinner, useConfig } from './ui';

export const methodNames: Record<string, string> = { get alipay() { return t("支付宝"); }, get wechat() { return t("微信支付"); }, usdt: 'USDT', get manual() { return t("人工充值"); } };
const rechargeMethods = ['usdt', 'manual'] as const;
export function OrderStatus({ order }: { order: Order }) {
  useLocale(); const status = order.status === 'pending' && new Date(order.expires_at).getTime() < Date.now() ? 'expired' : order.status; return <Badge tone={status === 'paid' ? 'green' : status === 'pending' ? 'amber' : 'gray'}>{{ paid: t("已到账"), pending: t("待支付"), expired: t("已超时") }[status] || status}</Badge>; }
function PaymentDialog({ order: initial, close }: { order: Order; close: () => void }) {
  useLocale();
  const cache = useQueryClient(); const { config } = useConfig(); const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [checked, setChecked] = useState(false); const { data: order = initial } = useQuery({ queryKey: ['order', initial.id], queryFn: () => api<Order>(`/orders/${initial.id}`), initialData: initial, refetchInterval: q => q.state.data?.status === 'paid' ? false : 5000 }); const expired = new Date(order.expires_at).getTime() < Date.now();
  const check = async () => { setBusy(true); setError(''); try { const o = await send<Order>(`/orders/${order.id}/check`, {}); cache.setQueryData(['order', order.id], o); cache.invalidateQueries({ queryKey: ['dashboard'] }); cache.invalidateQueries({ queryKey: ['orders'] }); cache.invalidateQueries({ queryKey: ['ledger'] }); setChecked(true); } catch (e) { setError(errorText(e)); } finally { setBusy(false); } };
  useEffect(() => { if (order.status === 'paid') for (const key of ['dashboard', 'orders', 'ledger', 'referrals']) cache.invalidateQueries({ queryKey: [key] }); }, [order.status, cache]);
  const paid = order.status === 'paid';
  return <Modal title={t("充值订单")} close={() => { cache.invalidateQueries(); close(); }}>
    {order.method === 'usdt' ? <UsdtPaymentDetails order={order} status={<OrderStatus order={order} />} showAddress={!paid} /> :
      <div className="payment-amount"><span>{order.payment_currency === 'CNY' ? t("应付金额（人民币）") : t("应付金额")}</span><strong>{order.payment_currency === 'CNY' ? `¥ ${cnyMoney(order.pay_cny_cents || 0)}` : `$ ${money(order.amount_micros)}`}</strong><OrderStatus order={order} /></div>}
    {(order.method !== 'usdt' || paid) && (order.bonus_micros || 0) > 0 && <div className="recharge-gift-summary"><div><span>{t("活动赠送")}</span><strong>+ ${rechargeMoney(order.bonus_micros)}</strong></div><div><span>{paid ? t("到账余额") : t("预计到账余额")}</span><strong>${rechargeMoney(order.amount_micros + order.bonus_micros)}</strong></div></div>}
    {paid ? <div className="payment-success"><span><Check size={32} /></span><h3>{t("充值已到账")}</h3><p>{config.metered_billing ? t("余额按实际用量扣费。") : t("余额可以购买会员，也可支付超额下载费用。")}</p></div> : <>
      {order.method === 'usdt' ? null : order.method === 'alipay' && order.payment_currency === 'CNY' ? <AlipayOrderDetails order={order} expired={expired} /> : order.method === 'manual' ? <div className="notice"><CreditCard size={20} /><div><strong>{t("等待管理员核对入账")}</strong><p>{t("请联系管理员核对线下转账。创建订单本身不会增加余额。")}</p>{config.support_email && <a href={`mailto:${config.support_email}`}>{config.support_email}</a>}</div></div> : <div className="notice">{t("此支付渠道已停用，请重新创建充值订单。")}</div>}
      {expired && <div className="form-error">{t("订单已超时，请勿继续转账。已付款但未到账时，请联系管理员核对。")}</div>}
      <FormError>{error}</FormError>
      {checked && !error && <p className="muted small">{t("尚未收到匹配的已确认付款。系统会继续自动核对，请稍后刷新。")}</p>}
      <div className="modal-footer"><button className="btn secondary" onClick={close}>{t("稍后查看")}</button><button className="btn primary" disabled={busy} onClick={check}><RefreshCw className={busy ? 'spin' : ''} size={16} />{busy ? t("核对中…") : t("检查到账状态")}</button></div>
    </>}
  </Modal>;
}
export function BillingPage() {
  useLocale();
  const { config } = useConfig(); const cache = useQueryClient(); const notify = useNotify(); const { data: d, error, refetch } = useQuery({ queryKey: ['dashboard'], queryFn: () => api<Dashboard>('/dashboard') }); const [tab, setTab] = useState('orders'); const [page, setPage] = useState(1); const { data: orders, error: ordersError, refetch: refetchOrders } = useQuery({ queryKey: ['orders', page], queryFn: () => api<Page<Order>>(`/orders?page=${page}`), enabled: tab === 'orders' }); const { data: ledger, error: ledgerError, refetch: refetchLedger } = useQuery({ queryKey: ['ledger', page], queryFn: () => api<Page<Ledger>>(`/ledger?page=${page}`), enabled: tab === 'ledger' }); const [recharge, setRecharge] = useState(false); const [amount, setAmount] = useState('100'); const [method, setMethod] = useState(rechargeMethods.find(m => config.payment_methods.includes(m)) || 'manual'); const [payment, setPayment] = useState<Order>(); const [purchase, setPurchase] = useState(false); const [busy, setBusy] = useState(false); const [formError, setFormError] = useState(''); const key = useRef(newKey());
  const openRecharge = (suggested = '100') => { setPurchase(false); setAmount(suggested); setRecharge(true); setFormError(''); key.current = newKey(); setMethod(rechargeMethods.find(m => config.payment_methods.includes(m)) || 'manual'); };
  const rechargeMicros = Math.round(Number(amount) * 1e6); const giftMicros = rechargeBonus(rechargeMicros, config);
  const [search, setSearch] = useSearchParams(); const returnOrder = search.get('order');
  useEffect(() => { if (!returnOrder) return; if (/^[a-f0-9]{32}$/.test(returnOrder)) api<Order>(`/orders/${returnOrder}`).then(setPayment).catch(e => notify(errorText(e), { type: 'error' })); setSearch({}, { replace: true }); }, [returnOrder, setSearch, notify]);
  const submit = async (e: FormEvent) => { e.preventDefault(); setBusy(true); setFormError(''); try { const o = await send<Order>('/orders', { amount, method }, 'POST', key.current); setRecharge(false); setPayment(o); cache.invalidateQueries(); } catch (e) { setFormError(errorText(e)); } finally { setBusy(false); } };
  if (error) return <ErrorState error={error} retry={() => refetch()} />; if (!d) return <Spinner />; const pro = config.plans.find(p => p.id === 'pro')!;
  const available = d.user.balance_micros - d.user.held_micros;
  const quotaRemaining = d.quota_remaining;
  return <div className="billing-page">
    <PageHeader eyebrow={config.metered_billing ? undefined : "BILLING & MEMBERSHIP"} title={config.metered_billing ? t("用量与账单") : t("会员与账单")} subtitle={config.metered_billing ? t("余额按实际用量扣费。") : t("选择套餐，按需充值，让每一次下载清晰可见。")} />
    {config.metered_billing ? <div className="billing-wallet-overview">
      <section className="billing-wallet" aria-label={t("账户余额")}>
        <div className="billing-wallet-heading"><span><Wallet size={17}/>{t("可用余额 ")}<small>USD</small></span><button className="btn primary" onClick={() => openRecharge()}><Plus size={15}/>{t("充值余额")}</button></div>
        <strong className="billing-wallet-amount">$ {money(available, 3)}</strong>
        {d.user.held_micros > 0 && <p className="billing-wallet-hold">{t("预留金额")} $ {money(d.user.held_micros, 6)}</p>}
      </section>
      <section className="billing-monthly-metric" aria-label={t("本月用量")}><span><ArrowDownToLine size={17}/>{t("本月用量")}</span><strong>{number(d.usage.free_used + d.usage.paid_used)}<small>{t("次")}</small></strong></section>
      <section className="billing-monthly-metric" aria-label={t("本月消费")}><span><CreditCard size={17}/>{t("本月消费")}<small>USD</small></span><strong>$ {money(d.usage.spent_micros, 3)}</strong></section>
    </div> : <div className="billing-top compact-billing-top">
      <section className="balance-panel compact-balance">
        <div className="balance-main"><span><Wallet size={16} />{t("可用余额 ")}<small>USD</small></span><strong>$ {money(available, 3)}</strong><p>{config.metered_billing ? t("APK 访问 UV") + " $" : t("超额下载 $")}{money(config.download_price_micros, 3)} {t(" / 次")}</p>{d.user.held_micros > 0 && <p>{config.metered_billing ? t("预留金额") + " $" : t("下载中预留 $")}{money(d.user.held_micros, 6)}</p>}</div>
        <button className="btn balance-recharge" onClick={() => openRecharge()}><Plus size={16} />{t("充值余额")}</button>
      </section>
      <section className="panel compact-quota">
        <div className="compact-quota-heading"><h2>{t("赠送下载额度")}</h2><Badge tone={d.plan.id === 'pro' ? 'violet' : 'green'}>{localizedPlanName(d.plan.name)}</Badge></div>
        <div className="quota-number"><strong>{number(quotaRemaining)}</strong><span>{t("次剩余 ")}<small>/ {number(d.quota_limit)} {t(" 次")}</small></span></div>
        <div className="progress-track"><span style={{ width: `${d.quota_limit ? Math.min(100, (d.quota_used + d.quota_reserved) / d.quota_limit * 100) : 0}%` }} /></div>
        <p>{config.metered_billing ? t("APK 访问与 PWA 安装共用 {0} 次赠送额度。", {0: number(d.quota_limit)}) : d.plan.id === 'free' ? t("注册赠送一次，剩余次数保留至用完") : t("{0} 到期", { 0: date(d.quota_resets_at) })}{d.quota_renews_at && t(" · {0} 发放续费额度", { 0: date(d.quota_renews_at) })}</p>
      </section>
    </div>}
    {config.metered_billing && <UsageRates config={config} compact collapsible/>}
{!config.metered_billing && <>    <div className="section-label"><h2>{t("选择适合你的套餐")}</h2><span>{t("美元计价 · 按月购买")}</span></div>
    <div className="membership-grid">
      {config.plans.map(p => <section key={p.id} className={`membership-card ${p.id}`}>
        <div className="membership-card-heading"><h3><i />{localizedPlanName(p.name)}</h3><span className="download-unit">${money(config.download_price_micros, 3)} {t(" / 次")}</span></div>
        <div className="membership-price"><span className="membership-label"><Wallet size={15} />{t("套餐价格")}</span><strong>${money(p.price_micros, 0)}<small>{p.id === 'free' ? t(" / 免费") : t(" / 月")}</small></strong></div>
        <div className="membership-metrics">
          <div><span><Boxes size={15} />{t("应用数量")}</span><strong>{number(p.app_limit)}<small> {t(" 个")}</small></strong></div>
          <div><span><HardDrive size={15} />{t("存储空间")}</span><strong>{bytes(p.storage_bytes)}</strong></div>
          <div><span><ArrowDownToLine size={15} />{t("赠送下载")}</span><strong>{number(p.download_limit)}<small> {t(" 次")}</small></strong></div>
        </div>
        {p.id !== 'free' && <div className="membership-term membership-security" title={t("在应用分流中开启安全检测后生效")}><ShieldCheck size={14} />{t("自动检查域名安全")}</div>}
        <div className="membership-term"><CalendarDays size={14} />{p.id === 'free' ? t("仅注册首月赠送，次月不再新增") : t("每个付费月赠送，续费后再次发放")}</div>
        {p.id === 'free' ? <button className="btn membership-buy full" disabled>{d.plan.id === 'free' ? t("当前套餐") : t("免费套餐")}</button> : <button className="btn membership-buy full" onClick={() => { setPurchase(true); setFormError(''); key.current = newKey(); }}><Zap size={15} />{d.plan.id === 'pro' ? t("续费{0}", { 0: localizedPlanName(p.name) }) : t("开通{0}", { 0: localizedPlanName(p.name) })}</button>}
      </section>)}
    </div>
    <div className="notice compact billing-rules"><ShieldCheck size={16} /><span>{t("赠送用尽后，所有套餐均按 $")}{money(config.download_price_micros, 3)} {t(" / 次扣费。付费套餐有效期为一个月；提前续费的额度在下个付费周期生效，付费周期内未使用的赠送额度不结转，免费注册赠送可保留至用完。")}</span></div>
</>}
    <section className="panel billing-records"><div className="tab-bar"><button className={tab === 'orders' ? 'active' : ''} onClick={() => { setTab('orders'); setPage(1); }}>{t("充值订单")}</button><button className={tab === 'ledger' ? 'active' : ''} onClick={() => { setTab('ledger'); setPage(1); }}>{t("余额流水")}</button></div>{tab === 'orders' ? ordersError ? <ErrorState error={ordersError} retry={() => refetchOrders()} /> : !orders ? <Spinner /> : !orders.data.length ? <Empty title={t("还没有充值订单")} detail={t("充值后，付款状态与订单详情将在这里展示。")} /> : <div className="table-wrap"><table><thead><tr><th>{t("订单编号")}</th><th>{t("充值金额")}</th><th>{t("支付方式")}</th><th>{t("状态")}</th><th>{t("创建时间")}</th><th /></tr></thead><tbody>{orders.data.map(o => <tr key={o.id}><td className="mono small">{o.id.slice(0, 16)}…</td><td className="semibold">$ {money(o.amount_micros)}{(o.bonus_micros || 0) > 0 && <small className="cell-sub success-text">{t("赠 $")}{rechargeMoney(o.bonus_micros)}</small>}</td><td>{methodNames[o.method]}</td><td><OrderStatus order={o} /></td><td className="muted">{date(o.created_at, true)}</td><td><button className="text-btn" onClick={() => setPayment(o)}>{t("查看详情 ")}<ArrowRight size={13} /></button></td></tr>)}</tbody></table></div> : ledgerError ? <ErrorState error={ledgerError} retry={() => refetchLedger()} /> : !ledger ? <Spinner /> : !ledger.data.length ? <Empty title={t("还没有余额变动")} detail={config.metered_billing ? t("充值、邀请返利与用量扣费会生成可追溯流水。") : t("充值、邀请返利、会员购买与下载扣费会生成可追溯流水。")} /> : <div className="table-wrap"><table><thead><tr><th>{t("类型 / 说明")}</th><th>{t("余额变动")}</th><th>{t("变动后余额")}</th><th>{t("时间")}</th></tr></thead><tbody>{ledger.data.map(l => <tr key={l.id}><td>{ledgerKind(l.kind) ? <><span>{ledgerKind(l.kind)}{l.balance_type === "gift" && <> · {t("赠送用量额度")}</>}</span>{l.note && <small className="cell-sub">{l.note}</small>}</> : l.note}<small className="cell-sub mono">{l.reference.slice(0, 28)}…</small></td><td className={l.amount_micros > 0 ? 'success-text' : ''}>{l.amount_micros > 0 ? '+' : '−'} ${ledgerKind(l.kind) ? rechargeMoney(Math.abs(l.amount_micros)) : money(Math.abs(l.amount_micros), 3)}</td><td>${ledgerKind(l.kind) ? rechargeMoney(l.balance_micros) : money(l.balance_micros, 3)}</td><td className="muted">{date(l.created_at, true)}</td></tr>)}</tbody></table></div>}<Pager page={page} total={(tab === 'orders' ? orders?.total : ledger?.total) || 0} setPage={setPage} /></section>{recharge && <Modal title={t("充值账户余额")} close={() => { if (!busy) setRecharge(false); }}><form onSubmit={submit}><p className="modal-intro">{config.metered_billing ? t("账户以美元计价，充值后按实际用量扣费。") : t("账户以美元（USD）计价，余额可用于购买会员与超额下载扣费。")}</p><Field label={t("充值金额（美元）")}><input type="number" min="1" max="10000" step="0.01" required value={amount} onChange={e => { setAmount(e.target.value); key.current = newKey(); }} /></Field><div className="amount-options">{['100', '200', '500', '1000'].map(n => <button type="button" key={n} className={amount === n ? 'selected' : ''} onClick={() => { setAmount(n); key.current = newKey(); }}>${n}{rechargeBonus(Number(n) * 1e6, config) > 0 && <span className="recharge-gift-badge" aria-label={t("赠送{0}%", { 0: rechargePercent(Number(n) * 1e6, config) })}>{t("赠送")}{rechargePercent(Number(n) * 1e6, config)}%</span>}</button>)}</div>{config.recharge_bonus_enabled && <div className="recharge-promotion"><p>{config.recharge_bonus_tiers.map(tier => t("满 ${0} 赠送{1}%", { 0: rechargeMoney(tier.threshold_micros), 1: tier.percent })).join(" · ")}{t("（按最高适用档位，不叠加）")}</p>{giftMicros > 0 && <div className="recharge-gift-summary"><div><span>{t("活动赠送")}</span><strong>+ ${rechargeMoney(giftMicros)}</strong></div><div><span>{t("预计到账余额")}</span><strong>${rechargeMoney(rechargeMicros + giftMicros)}</strong></div></div>}</div>}<fieldset className="payment-methods"><legend>{t("支付方式")}</legend><div className="payment-options">{rechargeMethods.map(m => { const available = config.payment_methods.includes(m); const Icon = m === 'usdt' ? Coins : Headset; return <button type="button" key={m} aria-label={methodNames[m]} aria-pressed={method === m} disabled={!available} className={method === m ? 'selected' : ''} onClick={() => { setMethod(m); key.current = newKey(); }}><span className={`payment-option-icon ${m}`} aria-hidden="true"><Icon size={15} strokeWidth={1.8} focusable="false" /></span><strong>{methodNames[m]}</strong>{(!available || m === 'manual') && <small>{!available ? t("暂未开通") : t("联系管理员")}</small>}</button>; })}</div></fieldset><FormError>{formError}</FormError><div className="modal-footer"><button type="button" className="btn secondary" onClick={() => setRecharge(false)}>{t("取消")}</button><button className="btn primary" disabled={busy}>{busy ? t("创建中…") : t("创建充值订单")}<ArrowRight size={15} /></button></div></form></Modal>}{payment && <PaymentDialog order={payment} close={() => { setPayment(undefined); cache.invalidateQueries(); }} />}
    {!config.metered_billing && purchase && <Modal title={d.plan.id === 'pro' ? t("续费{0}", { 0: localizedPlanName(pro.name) }) : t("开通{0}", { 0: localizedPlanName(pro.name) })} close={() => { if (!busy) setPurchase(false); }}>
      <div className="purchase-summary"><Zap size={30} /><h3>{localizedPlanName(pro.name)} {t(" · 一个月")}</h3><strong>$ {money(pro.price_micros)}</strong><p>{t("从账户余额扣除。每个付费周期赠送 ")}{number(pro.download_limit)} {t(" 次下载。当前可用余额 $")}{money(available, 3)}。</p>{d.user.pro_until && new Date(d.user.pro_until).getTime() > Date.now() && <p>{t("当前有效期至 ")}{date(d.user.pro_until)}{t("，本次续费在该日期之后生效并发放新额度。")}</p>}</div>
      <FormError>{formError}</FormError>
      <div className="modal-footer"><button className="btn secondary" onClick={() => setPurchase(false)}>{t("取消")}</button><button className="btn primary" disabled={busy} onClick={async () => {
        if (available < pro.price_micros) { openRecharge(String(Math.max(100, Math.ceil((pro.price_micros - available) / 1e6)))); return; }
        setBusy(true); setFormError('');
        try { await send('/subscription', {}, 'POST', key.current); setPurchase(false); cache.invalidateQueries(); notify(t("套餐购买成功"), { type: 'success' }); }
        catch (e) { if (e instanceof APIError && e.status === 402) { openRecharge(); cache.invalidateQueries({ queryKey: ['dashboard'] }); } else setFormError(errorText(e)); }
        finally { setBusy(false); }
      }}>{available < pro.price_micros ? t("余额不足，请先充值") : busy ? t("处理中…") : t("确认从余额支付")}</button></div>
    </Modal>}
  </div>;
}
