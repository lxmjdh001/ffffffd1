import { useEffect, useState } from 'react';
import { send } from './api';
import type { Order, SettingsResponse } from './types';
import { Badge, Field, FormError, errorText } from './ui';
import { rechargeMoney } from './recharge';

export const ALIPAY_FEE_PERCENT = 5;
export const DEFAULT_ALIPAY_RATE = 6650000;
export const cnyMoney = (cents: number) => (cents / 100).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });

export function alipayQuote(amountMicros: number, rateMicros: number) {
  if (!Number.isSafeInteger(amountMicros) || amountMicros < 1000000 || amountMicros > 10000 * 1000000 || amountMicros % 10000 !== 0 || !Number.isSafeInteger(rateMicros) || rateMicros < 10000 || rateMicros > 1000 * 1000000) return undefined;
  const feeMicros = amountMicros * ALIPAY_FEE_PERCENT / 100;
  const denominator = 10000000000n;
  const payCNYCents = Number((BigInt(amountMicros + feeMicros) * BigInt(rateMicros) + denominator - 1n) / denominator);
  return { feeMicros, payCNYCents };
}

export function AlipayQuote({ amountMicros, rateMicros }: { amountMicros: number; rateMicros: number }) {
  const quote = alipayQuote(amountMicros, rateMicros);
  return <div className="notice compact" aria-label="支付宝付款预估"><div><strong>支付宝人民币付款</strong><p>充值本金 ${rechargeMoney(amountMicros || 0)} · 手续费 {ALIPAY_FEE_PERCENT}%{quote ? `（$${rechargeMoney(quote.feeMicros)}）` : ''}</p><p>汇率：1 USDT = ¥{rateMicros / 1000000}，预计应付 <strong>{quote ? `¥${cnyMoney(quote.payCNYCents)}` : '—'}</strong></p><p>按最终金额向上取整到分，以创建订单后显示的金额为准。手续费不计入余额、活动赠送或邀请返利。</p></div></div>;
}

export function AlipayOrderDetails({ order, expired }: { order: Order; expired: boolean }) {
  let validURL = false;
  try { validURL = new URL(order.pay_url).protocol === 'https:'; } catch { /* Missing or legacy link. */ }
  return <div className="notice"><div><strong>支付宝付款明细</strong><p>充值本金 ${rechargeMoney(order.amount_micros)} · 手续费 {ALIPAY_FEE_PERCENT}%（${rechargeMoney(order.fee_micros || 0)}）</p><p>订单汇率：1 USDT = ¥{(order.alipay_rate_micros || 0) / 1000000}</p><p>到账本金为 ${rechargeMoney(order.amount_micros)}，活动赠送另计；手续费不参与赠送或邀请返利。下单后汇率和金额已锁定。</p>{order.status !== 'paid' && !expired && (validURL ? <a className="btn primary" href={order.pay_url} target="_blank" rel="noopener noreferrer">前往支付宝付款</a> : <p role="alert">付款链接不可用，请联系管理员核对此订单。</p>)}{order.status !== 'paid' && <p>付款后返回此页自动刷新，以后台通知到账为准。</p>}</div></div>;
}

export function AlipaySettings({ data, saved }: { data: SettingsResponse; saved: (data: SettingsResponse) => void }) {
  const initialRate = data.settings.alipay_cny_rate_micros ?? DEFAULT_ALIPAY_RATE;
  const [rate, setRate] = useState(String(initialRate / 1000000));
  const [busy, setBusy] = useState(false); const [error, setError] = useState(''); const [success, setSuccess] = useState(false);
  useEffect(() => { setRate(String(initialRate / 1000000)); }, [initialRate]);
  const save = async () => {
    const value = Number(rate);
    if (!Number.isFinite(value) || value < 0.01 || value > 1000 || !/^\d+(?:\.\d{1,6})?$/.test(rate)) { setError('汇率须为 0.01–1000，最多六位小数'); return; }
    setBusy(true); setError(''); setSuccess(false);
    try { saved(await send<SettingsResponse>('/admin/settings', { section: 'alipay', settings: { alipay_cny_rate_micros: Math.round(value * 1000000) } }, 'PUT')); setSuccess(true); }
    catch (e) { setError(errorText(e)); }
    finally { setBusy(false); }
  };
  return <section className="panel padded settings-panel"><div className="panel-heading"><h2>支付宝收款配置</h2><Badge tone={data.runtime.futoon_configured ? 'green' : 'gray'}>{data.runtime.futoon_configured ? '富通已配置' : '富通未配置'}</Badge></div><Field label="支付宝汇率（人民币 / USDT）" hint="例如 6.65 表示 1 USDT = 6.65 元；修改仅影响新订单。"><input required disabled={busy} type="number" min="0.01" max="1000" step="0.000001" value={rate} onChange={e => { setRate(e.target.value); setError(''); setSuccess(false); }} /></Field><p className="muted small">支付宝手续费固定为 5%，商户密钥仅由服务器环境变量配置，不在此页面显示或修改。USDT 充值汇率及收款地址不受影响。</p><AlipayQuote amountMicros={100000000} rateMicros={Math.round(Number(rate) * 1000000)} /><div className="settings-save-row"><button type="button" className="btn primary" disabled={busy} onClick={save}>{busy ? '保存中…' : '保存支付宝配置'}</button>{success && <span className="success-text" role="status">已保存</span>}</div><FormError>{error}</FormError></section>;
}
