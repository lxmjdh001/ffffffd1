import { useRef, useState, type FormEvent } from 'react';
import { usePermissions } from 'react-admin';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Percent } from 'lucide-react';
import { api, send } from './api';
import { t, useLocale } from './i18n';
import type { CommissionRates, CommissionSettings, User } from './types';
import { rechargeMoney } from './recharge';
import { Badge, ErrorState, errorText, Field, FormError, Modal, number, PageHeader, Spinner } from './ui';
import './commission-settings.css';

type RateDraft = [string, string, string];
const rateLabels = () => [t('一级返佣比例（%）'), t('二级返佣比例（%）'), t('三级返佣比例（%）')];
const rateDraft = (rates: CommissionRates): RateDraft => rates.map(String) as RateDraft;
export const validCommissionRate = (value: string) => /^(?:0|[1-9]\d{0,2})(?:\.\d{1,2})?$/.test(value.trim()) && Number(value.trim()) <= 100;

function RateFields({ rates, change, disabled }: { rates: RateDraft; change: (rates: RateDraft) => void; disabled: boolean }) {
  return <div className="commission-rate-fields">{rateLabels().map((label, index) => <Field key={index} label={label}><div className="commission-rate-input"><input aria-label={label} required inputMode="decimal" autoComplete="off" maxLength={6} placeholder={index === 0 ? '10' : '2'} value={rates[index]} disabled={disabled} onChange={event => change(rates.map((value, i) => i === index ? event.target.value : value) as RateDraft)} /><span>%</span></div></Field>)}</div>;
}

function RateSummary({ rates }: { rates: CommissionRates }) {
  return <span className="commission-rate-summary">{rates.map((rate, index) => <span key={index}><small>{[t('一级'), t('二级'), t('三级')][index]}</small><strong>{number(rate)}%</strong></span>)}</span>;
}

function CommissionSettingsForm({ initial }: { initial: CommissionSettings }) {
  const [rates, setRates] = useState<RateDraft>(() => rateDraft(initial.rates));
  const [error, setError] = useState('');
  const [success, setSuccess] = useState(false);
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const cache = useQueryClient();
  const valid = rates.every(validCommissionRate);
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (submitting.current || !valid) return;
    submitting.current = true; setBusy(true); setError(''); setSuccess(false);
    try {
      const result = await send<CommissionSettings>('/admin/commission-settings', { rates: rates.map(value => value.trim()) }, 'PUT');
      setRates(rateDraft(result.rates)); setSuccess(true); cache.setQueryData(['commission-settings'], result);
      await Promise.all([cache.invalidateQueries({ queryKey: ['admin', 'users'] }), cache.invalidateQueries({ queryKey: ['referrals'] })]);
    } catch (failure) { setError(errorText(failure)); }
    finally { submitting.current = false; setBusy(false); }
  };
  return <form onSubmit={submit}><RateFields rates={rates} disabled={busy} change={value => { setRates(value); setError(''); setSuccess(false); }} /><p className="commission-form-hint">{t('三个层级均需设置，范围 0–100%，最多 2 位小数。')}</p><FormError>{error}</FormError>{success && <p className="notice compact" role="status">{t('全局佣金比例已保存，将用于后续充值返利。')}</p>}<div className="commission-save-row"><button className="btn primary" disabled={busy || !valid}>{busy ? t('保存中…') : t('保存全局比例')}</button></div></form>;
}

function CommissionSettingsContent() {
  const { data, error, refetch } = useQuery({ queryKey: ['commission-settings'], queryFn: () => api<CommissionSettings>('/admin/commission-settings') });
  return <><PageHeader eyebrow="PLATFORM ADMIN" title={t('佣金设置')} subtitle={t('管理三级邀请返利比例，并为指定用户设置专属比例。')} action={<Badge tone="violet">{t('管理员视图')}</Badge>} /><section className="panel padded commission-settings"><div className="panel-heading"><div><h2><Percent size={19} />{t('全局返佣比例')}</h2><p>{t('未设置个人比例的用户，按全局比例计算邀请返利。')}</p></div></div><div className="notice compact">{t('个人设置优先于全局设置，可高于或低于全局比例。调整仅影响后续充值到账产生的返利，历史佣金不变。')}</div>{error ? <ErrorState error={error} retry={() => refetch()} /> : !data ? <Spinner /> : <CommissionSettingsForm initial={data} />}</section><section className="panel padded commission-settings commission-user-guide"><h2>{t('个人佣金设置')}</h2><p>{t('在用户管理中可查看每位用户的邀请人数、累计佣金及当前比例，单独设置个人比例，或恢复跟随全局。')}</p></section></>;
}

export function CommissionSettingsPage() {
  useLocale();
  const { permissions, isPending } = usePermissions();
  if (isPending) return <Spinner />;
  if (permissions !== 'admin') return <ErrorState error={new Error(t('此页面需要管理员权限'))} />;
  return <CommissionSettingsContent />;
}

function UserCommissionDialog({ user, close }: { user: User; close: () => void }) {
  useLocale();
  const [rates, setRates] = useState<RateDraft>(() => rateDraft(user.referral_rates || [10, 2, 2]));
  const [reset, setReset] = useState(false);
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const cache = useQueryClient();
  const global = useQuery({ queryKey: ['commission-settings'], queryFn: () => api<CommissionSettings>('/admin/commission-settings'), enabled: reset });
  const valid = reset ? !!global.data && !global.error : rates.every(validCommissionRate);
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (submitting.current || !valid) return;
    submitting.current = true; setBusy(true); setError('');
    try {
      await send(`/admin/users/${user.id}/referral-rates`, reset ? undefined : { rates: rates.map(value => value.trim()) }, reset ? 'DELETE' : 'PUT');
      await Promise.all([cache.invalidateQueries({ queryKey: ['admin', 'users'] }), cache.invalidateQueries({ queryKey: ['referrals'] })]); close();
    } catch (failure) { setError(errorText(failure)); }
    finally { submitting.current = false; setBusy(false); }
  };
  return <Modal title={t('设置个人佣金')} close={() => { if (!submitting.current) close(); }}><form onSubmit={submit}><p className="modal-intro">{user.name} · {user.email}</p><div className="commission-current"><span>{t('当前比例来源')}</span><Badge tone={user.referral_rates_overridden ? 'violet' : 'gray'}>{user.referral_rates_overridden ? t('个人设置') : t('跟随全局')}</Badge></div><div className="commission-mode"><button type="button" className={`btn ${!reset ? 'primary' : 'secondary'}`} disabled={busy} onClick={() => { setReset(false); setError(''); }}>{t('设置个人比例')}</button><button type="button" className={`btn ${reset ? 'primary' : 'secondary'}`} disabled={busy} onClick={() => { setReset(true); setError(''); }}>{t('恢复全局设置')}</button></div>{reset ? <><p className="notice compact">{t('恢复后将跟随平台全局比例，后续全局调整会自动生效。')}</p>{global.error ? <ErrorState error={global.error} retry={() => global.refetch()} /> : !global.data ? <Spinner /> : <div className="commission-reset-summary"><span>{t('当前全局比例')}</span><RateSummary rates={global.data.rates} /></div>}</> : <><RateFields rates={rates} disabled={busy} change={value => { setRates(value); setError(''); }} /><p className="commission-form-hint">{t('三个层级均需设置，范围 0–100%，最多 2 位小数。')}</p></>}<p className="small muted">{t('调整仅影响该用户后续获得的充值返利，历史佣金不变。')}</p><FormError>{error}</FormError><div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={close}>{t('取消')}</button><button className="btn primary" disabled={busy || !valid}>{busy ? t('保存中…') : reset ? t('确认恢复全局') : t('保存个人比例')}</button></div></form></Modal>;
}

export function UserReferralCommissionCells({ user }: { user: User }) {
  useLocale();
  const [open, setOpen] = useState(false);
  return <><td className="commission-team-cell"><span>{t('直接邀请')} <strong>{number(user.direct_invite_count || 0)}</strong></span><small className="cell-sub">{t('团队人数')} {number(user.team_invite_count || 0)}</small></td><td className="nowrap">$ {rechargeMoney(user.total_commission_micros || 0)}</td><td className="commission-user-cell"><RateSummary rates={user.referral_rates || [10, 2, 2]} /><span className="commission-source"><Badge tone={user.referral_rates_overridden ? 'violet' : 'gray'}>{user.referral_rates_overridden ? t('个人设置') : t('跟随全局')}</Badge><button className="text-btn" onClick={() => setOpen(true)}>{t('设置佣金')}</button></span>{open && <UserCommissionDialog user={user} close={() => setOpen(false)} />}</td></>;
}
