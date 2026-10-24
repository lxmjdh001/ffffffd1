import { useRef, useState, type FormEvent } from 'react';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { usePermissions } from 'react-admin';
import { ArrowDownToLine, Search } from 'lucide-react';
import { api, newKey, send } from './api';
import { t, useLocale } from './i18n';
import type { Page, Referrals, ReferralWithdrawal, ReferralWithdrawalStatus } from './types';
import { rechargeMoney } from './recharge';
import { Badge, date, Empty, ErrorState, errorText, Field, FormError, Modal, number, PageHeader, Pager, Spinner } from './ui';
import './referral-withdrawals.css';

const minimumWithdrawal = 100_000_000;
const pageSize = 10;
const statusLabels = (): Record<ReferralWithdrawalStatus, string> => ({ pending: t('待审核'), approved: t('待打款'), paid: t('已打款'), rejected: t('已驳回') });

export function withdrawalAmountMicros(value: string): number | undefined {
  if (!/^(?:0|[1-9]\d*)(?:\.\d{1,6})?$/.test(value.trim())) return undefined;
  const [whole, decimal = ''] = value.trim().split('.');
  const amount = Number(whole) * 1_000_000 + Number(decimal.padEnd(6, '0'));
  return Number.isSafeInteger(amount) ? amount : undefined;
}

function WithdrawalStatus({ status }: { status: ReferralWithdrawalStatus }) {
  return <Badge tone={status === 'paid' ? 'green' : status === 'rejected' ? 'red' : status === 'approved' ? 'violet' : 'gray'}>{statusLabels()[status] || status}</Badge>;
}

function WithdrawalDetails({ withdrawal }: { withdrawal: ReferralWithdrawal }) {
  return <div className="withdrawal-details"><span className="withdrawal-address mono">{withdrawal.address}</span><small>USDT · TRC20</small>{withdrawal.review_note && <p>{t('审核备注：')}{withdrawal.review_note}</p>}{withdrawal.payout_reference && <p>{t('打款交易号：')}<span className="mono">{withdrawal.payout_reference}</span></p>}{withdrawal.reviewed_at && <small>{t('审核时间：')}{date(withdrawal.reviewed_at, true)}</small>}{withdrawal.paid_at && <small>{t('打款时间：')}{date(withdrawal.paid_at, true)}</small>}</div>;
}

export function ReferralWithdrawals({ summary, open, close }: { summary: Referrals; open: boolean; close: () => void }) {
  useLocale();
  const cache = useQueryClient();
  const [page, setPage] = useState(1);
  const [amount, setAmount] = useState('');
  const [address, setAddress] = useState('');
  const [error, setError] = useState('');
  const [success, setSuccess] = useState(false);
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const attempt = useRef<{ payload: string; key: string } | null>(null);
  const available = summary.withdrawal_reconciliation_required ? 0 : summary.withdrawal_available_micros || 0;
  const minimum = summary.withdrawal_minimum_micros || minimumWithdrawal;
  const amountMicros = withdrawalAmountMicros(amount);
  const canSubmit = amountMicros !== undefined && amountMicros >= minimum && amountMicros <= available && /^T[1-9A-HJ-NP-Za-km-z]{33}$/.test(address.trim());
  const query = useQuery({ queryKey: ['referral-withdrawals', page], queryFn: () => api<Page<ReferralWithdrawal>>(`/referral-withdrawals?${new URLSearchParams({ page: String(page), per_page: String(pageSize) })}`), placeholderData: keepPreviousData });
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (submitting.current) return;
    setError('');
    if (!canSubmit) { setError(t('请核对提现金额及 USDT（TRC20）收款地址。')); return; }
    const body = { amount: amount.trim(), address: address.trim() };
    const payload = JSON.stringify({ amount_micros: amountMicros, address: body.address });
    if (attempt.current?.payload !== payload) attempt.current = { payload, key: newKey() };
    submitting.current = true; setBusy(true); setSuccess(false);
    try {
      await send<ReferralWithdrawal>('/referral-withdrawals', body, 'POST', attempt.current.key);
      attempt.current = null; setAmount(''); setAddress(''); setSuccess(true); setPage(1); close();
      await Promise.all([cache.invalidateQueries({ queryKey: ['referrals'] }), cache.invalidateQueries({ queryKey: ['referral-withdrawals'] }), cache.invalidateQueries({ queryKey: ['dashboard'] }), cache.invalidateQueries({ queryKey: ['me'] })]);
    } catch (failure) { setError(errorText(failure)); }
    finally { submitting.current = false; setBusy(false); }
  };
  return <section className="panel referral-withdrawals" aria-labelledby="referral-withdrawals-heading">
    <div className="panel-heading"><div><h2 id="referral-withdrawals-heading">{t('返利提现')}</h2><p>{t('单笔满 $100 可申请，管理员审核后通过 USDT（TRC20）人工打款。')}</p></div><ArrowDownToLine size={22} /></div>
    <div className="withdrawal-balances"><div><span>{t('可提现额度')}</span><strong>$ {rechargeMoney(available)}</strong></div><div><span>{t('审核中 / 待打款')}</span><strong>$ {rechargeMoney(summary.withdrawal_pending_micros || 0)}</strong></div><div><span>{t('已提现额度')}</span><strong>$ {rechargeMoney(summary.withdrawal_paid_micros || 0)}</strong></div></div>
    <p className="withdrawal-explanation">{t('仅未消耗的邀请返利可提现。提交申请后冻结对应额度，驳回后退回可用余额。')}</p>
    {summary.withdrawal_reconciliation_required && <p className="notice compact" role="alert">{t('提现资格需要人工核对，请联系客服。')}</p>}
    {success && <p className="notice compact" role="status">{t('提现申请已提交，请等待管理员审核。')}</p>}
    <div className="withdrawal-records-heading"><h3>{t('提现记录')}</h3></div>
    {query.error ? <ErrorState error={query.error} retry={() => query.refetch()} /> : !query.data ? <Spinner /> : !query.data.data.length ? <Empty title={t('暂无提现记录')} detail={t('可提现额度达到 $100 后，可提交提现申请。')} /> : <div className="table-wrap"><table><thead><tr><th>{t('申请金额')}</th><th>{t('收款信息 / 处理记录')}</th><th>{t('状态')}</th><th>{t('申请时间')}</th></tr></thead><tbody>{query.data.data.map(withdrawal => <tr key={withdrawal.id}><td className="nowrap">$ {rechargeMoney(withdrawal.amount_micros)}</td><td><WithdrawalDetails withdrawal={withdrawal} /></td><td><WithdrawalStatus status={withdrawal.status} /></td><td className="muted nowrap">{date(withdrawal.created_at, true)}<small className="cell-sub mono">{withdrawal.id}</small></td></tr>)}</tbody></table></div>}
    <fieldset className="referral-pager" disabled={query.isFetching}><Pager page={page} total={query.data?.total || 0} setPage={setPage} size={pageSize} /></fieldset>
    {open && <Modal title={t('申请提现')} close={() => { if (!submitting.current) close(); }}><form onSubmit={submit}>
      <div className="notice compact">{t('可提现额度：')}<strong>$ {rechargeMoney(available)}</strong><br />{t('单笔最低提现：')}<strong>$ {rechargeMoney(minimum)}</strong></div>
      {available < minimum && <p className="withdrawal-threshold" role="status">{t('可提现额度尚未达到 $100，请达到门槛后再申请。')}</p>}
      <Field label={t('提现金额（美元）')} hint={t('最多支持 6 位小数，申请金额不可超过可提现额度。')}><input aria-label={t('提现金额（美元）')} required inputMode="decimal" autoComplete="off" placeholder="100" maxLength={24} value={amount} disabled={busy} onChange={event => setAmount(event.target.value)} /></Field>
      <Field label={t('USDT（TRC20）收款地址')} hint={t('仅支持 TRON 网络，请仔细核对地址，打款后无法撤回。')}><input aria-label={t('USDT（TRC20）收款地址')} required autoComplete="off" spellCheck={false} maxLength={34} pattern="T[1-9A-HJ-NP-Za-km-z]{33}" placeholder="T…" value={address} disabled={busy} onChange={event => setAddress(event.target.value)} /></Field>
      <p className="small muted">{t('提交后会冻结对应额度；管理员审核通过后人工打款，审核驳回则退回。')}</p>
      <FormError>{error}</FormError><div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={close}>{t('取消')}</button><button className="btn primary" disabled={busy || !canSubmit}>{busy ? t('提交中…') : t('提交提现申请')}</button></div>
    </form></Modal>}
  </section>;
}

type ReviewAction = 'approve' | 'reject' | 'paid';
const actionLabels = (): Record<ReviewAction, string> => ({ approve: t('审核通过'), reject: t('驳回申请'), paid: t('确认已线下打款') });

function WithdrawalReview({ withdrawal, action, close }: { withdrawal: ReferralWithdrawal; action: ReviewAction; close: () => void }) {
  useLocale();
  const cache = useQueryClient();
  const [note, setNote] = useState('');
  const [transactionID, setTransactionID] = useState('');
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const valid = (action !== 'reject' || note.trim().length >= 4) && (action !== 'paid' || /^[a-fA-F0-9]{64}$/.test(transactionID.trim()));
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (submitting.current || !valid) return;
    submitting.current = true; setBusy(true); setError('');
    try {
      await send(`/admin/referral-withdrawals/${withdrawal.id}/review`, { action, note: note.trim(), ...(action === 'paid' ? { transaction_id: transactionID.trim() } : {}) });
      await Promise.all([cache.invalidateQueries({ queryKey: ['admin-referral-withdrawals'] }), cache.invalidateQueries({ queryKey: ['referrals'] }), cache.invalidateQueries({ queryKey: ['referral-withdrawals'] })]);
      close();
    } catch (failure) { setError(errorText(failure)); }
    finally { submitting.current = false; setBusy(false); }
  };
  return <Modal title={actionLabels()[action]} close={() => { if (!submitting.current) close(); }}><form onSubmit={submit}>
    <div className="notice compact"><strong>$ {rechargeMoney(withdrawal.amount_micros)}</strong> · {withdrawal.user_name || withdrawal.user_id}<br /><span className="withdrawal-address mono">{withdrawal.address}</span><br />USDT · TRC20</div>
    {action === 'approve' && <p className="modal-intro">{t('通过后进入待打款状态，额度继续冻结。请完成线下转账后再确认打款。')}</p>}
    {action === 'reject' && <p className="modal-intro">{t('驳回后，冻结的提现额度将退回该用户可用余额。')}</p>}
    {action === 'paid' && <><p className="modal-intro">{t('请确认已向上述地址完成线下转账。此操作仅登记打款结果，不会自动转账。')}</p><Field label={t('打款交易号')} hint={t('填写已完成 TRC20 转账的 64 位十六进制交易哈希。')}><input aria-label={t('打款交易号')} required minLength={64} maxLength={64} pattern="[a-fA-F0-9]{64}" autoComplete="off" value={transactionID} disabled={busy} onChange={event => setTransactionID(event.target.value)} /></Field></>}
    <Field label={action === 'reject' ? t('驳回原因') : t('审核备注（选填）')} hint={action === 'reject' ? t('至少填写 4 个字，用户可查看该原因。') : undefined}><textarea aria-label={action === 'reject' ? t('驳回原因') : t('审核备注（选填）')} required={action === 'reject'} minLength={action === 'reject' ? 4 : undefined} maxLength={1000} rows={3} disabled={busy} value={note} onChange={event => setNote(event.target.value)} /></Field>
    <FormError>{error}</FormError><div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={close}>{t('取消')}</button><button className="btn primary" disabled={busy || !valid}>{busy ? t('处理中…') : actionLabels()[action]}</button></div>
  </form></Modal>;
}

function AdminWithdrawalList() {
  const [page, setPage] = useState(1);
  const [status, setStatus] = useState('');
  const [search, setSearch] = useState('');
  const [input, setInput] = useState('');
  const [review, setReview] = useState<{ withdrawal: ReferralWithdrawal; action: ReviewAction }>();
  const { data, error, refetch, isFetching } = useQuery({ queryKey: ['admin-referral-withdrawals', page, status, search], queryFn: () => api<Page<ReferralWithdrawal>>(`/admin/referral-withdrawals?${new URLSearchParams({ page: String(page), per_page: '20', status, q: search })}`), placeholderData: keepPreviousData });
  return <><PageHeader eyebrow="PLATFORM ADMIN" title={t('提现记录与审核')} subtitle={t('审核邀请返利提现申请，登记 USDT（TRC20）人工打款结果。')} action={<Badge tone="violet">{t('管理员视图')}</Badge>} /><section className="panel admin-withdrawals"><div className="table-toolbar"><div className="table-title">{t('提现记录')} <span>{number(data?.total || 0)}</span></div><div className="withdrawal-filters"><form className="search-box" onSubmit={event => { event.preventDefault(); setSearch(input.trim()); setPage(1); }}><Search size={16} /><input aria-label={t('搜索提现记录')} placeholder={t('搜索用户名称或邮箱')} value={input} onChange={event => setInput(event.target.value)} /><button className="sr-only">{t('搜索')}</button></form><select aria-label={t('提现状态筛选')} value={status} onChange={event => { setStatus(event.target.value); setPage(1); }}><option value="">{t('全部状态')}</option>{Object.entries(statusLabels()).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></div></div>
    {error ? <ErrorState error={error} retry={() => refetch()} /> : !data ? <Spinner /> : !data.data.length ? <Empty title={t('暂无匹配的提现记录')} /> : <div className="table-wrap"><table><thead><tr><th>{t('申请人 / 编号')}</th><th>{t('申请金额')}</th><th>{t('收款信息 / 处理记录')}</th><th>{t('状态')}</th><th>{t('申请时间')}</th><th className="right">{t('审核操作')}</th></tr></thead><tbody>{data.data.map(withdrawal => <tr key={withdrawal.id}><td><strong>{withdrawal.user_name || withdrawal.user_id}</strong><small className="cell-sub">{withdrawal.user_email}</small><small className="cell-sub mono">{withdrawal.id}</small></td><td className="nowrap">$ {rechargeMoney(withdrawal.amount_micros)}</td><td><WithdrawalDetails withdrawal={withdrawal} /></td><td><WithdrawalStatus status={withdrawal.status} /></td><td className="muted nowrap">{date(withdrawal.created_at, true)}</td><td><div className="row-actions wrap">{withdrawal.status === 'pending' && <button className="text-btn" disabled={isFetching} onClick={() => setReview({ withdrawal, action: 'approve' })}>{t('审核通过')}</button>}{withdrawal.status === 'approved' && <button className="text-btn" disabled={isFetching} onClick={() => setReview({ withdrawal, action: 'paid' })}>{t('登记打款')}</button>}{(withdrawal.status === 'pending' || withdrawal.status === 'approved') && <button className="text-btn danger-text" disabled={isFetching} onClick={() => setReview({ withdrawal, action: 'reject' })}>{t('驳回')}</button>}{(withdrawal.status === 'paid' || withdrawal.status === 'rejected') && <span className="muted small">{t('已完成')}</span>}</div></td></tr>)}</tbody></table></div>}
    <fieldset className="referral-pager" disabled={isFetching}><Pager page={page} total={data?.total || 0} setPage={setPage} /></fieldset></section>{review && <WithdrawalReview {...review} close={() => setReview(undefined)} />}</>;
}

export function AdminReferralWithdrawalsPage() {
  useLocale();
  const { permissions, isPending } = usePermissions();
  if (isPending) return <Spinner />;
  if (permissions !== 'admin') return <ErrorState error={new Error(t('此页面需要管理员权限'))} />;
  return <AdminWithdrawalList />;
}
