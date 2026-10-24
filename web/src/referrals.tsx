// i18n: static UI literals
import { t, useLocale } from './i18n';
import { useState } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Check, Copy, Gift, Link2, Users, Wallet } from 'lucide-react';
import { api } from './api';
import type { Referrals } from './types';
import { rechargeMoney } from './recharge';
import { Badge, date, Empty, ErrorState, Field, FormError, number, PageHeader, Pager, Spinner, useConfig } from './ui';
import './referrals.css';
import { ReferralWithdrawals } from './referral-withdrawals';

const pageSize = 10;
const levelNames = () => [t("一级"), t("二级"), t("三级")];

export function ReferralsPage() {
  useLocale(); const config=useConfig()?.config;
  const [rewardsPage, setRewardsPage] = useState(1);
  const [membersPage, setMembersPage] = useState(1);
  const [copyMessage, setCopyMessage] = useState('');
  const [copyError, setCopyError] = useState('');
  const [withdrawalOpen, setWithdrawalOpen] = useState(false);
  const { data, error, refetch, isFetching } = useQuery({
    queryKey: ['referrals', rewardsPage, membersPage],
    queryFn: () => api<Referrals>(`/referrals?${new URLSearchParams({ rewards_page: String(rewardsPage), members_page: String(membersPage), per_page: String(pageSize) })}`),
    placeholderData: keepPreviousData,
  });

  const copy = async (value: string, label: string) => {
    setCopyMessage(''); setCopyError('');
    try { await navigator.clipboard.writeText(value); setCopyMessage(t("{0}已复制", { 0: label })); }
    catch { setCopyError(t("复制失败，请点击输入框选择内容，手动复制。")); }
  };

  if (!data) return error ? <ErrorState error={error} retry={() => refetch()} /> : <Spinner />;
  return <div className="referrals-page">
    <PageHeader eyebrow="INVITE & EARN" title={t("邀请好友")} subtitle={t("分享邀请链接，享受长期邀请返利。")} />
    <div className="referral-stats">
      <section className="stat-card"><div className="stat-heading"><span>{t("累计返利额度")}</span><span className="stat-icon green"><Wallet size={20} /></span></div><div className="stat-value">$ {rechargeMoney(data.total_reward_micros)}</div><div className="stat-footer referral-withdraw-footer"><span>{t("已加入账户可消费余额")}</span><button type="button" className="text-btn" onClick={() => setWithdrawalOpen(true)}>{t("提现")}</button></div></section>
      <section className="stat-card"><div className="stat-heading"><span>{t("直接邀请好友")}</span><span className="stat-icon violet"><Link2 size={20} /></span></div><div className="stat-value">{number(data.direct_count)}<small>{t("人")}</small></div><div className="stat-footer">{t("通过你的邀请码注册的好友")}</div></section>
      <section className="stat-card"><div className="stat-heading"><span>{t("团队下级人数")}</span><span className="stat-icon blue"><Users size={20} /></span></div><div className="stat-value">{number(data.team_count)}<small>{t("人")}</small></div><div className="stat-footer">{t("下级邀请好友总数")}</div></section>
    </div>
    <ReferralWithdrawals summary={data} open={withdrawalOpen} close={() => setWithdrawalOpen(false)} />
    <section className="panel padded referral-share">
      <div className="panel-heading"><div><h2>{t("分享你的专属邀请")}</h2><p>{t("好友注册时填写邀请码，即可建立长期邀请关系。")}</p></div><Gift size={23} /></div>
      <div className="referral-share-fields">
        <Field label={t("我的邀请码")}><div className="referral-copy-row"><input aria-label={t("我的邀请码")} readOnly value={data.invite_code} onFocus={e => e.currentTarget.select()} /><button type="button" className="btn secondary" onClick={() => copy(data.invite_code, t("邀请码"))}><Copy size={15} />{t("复制邀请码")}</button></div></Field>
        <Field label={t("邀请链接")}><div className="referral-copy-row"><input aria-label={t("邀请链接")} readOnly value={data.invite_url} onFocus={e => e.currentTarget.select()} /><button type="button" className="btn primary" onClick={() => copy(data.invite_url, t("邀请链接"))}><Copy size={15} />{t("复制链接")}</button></div></Field>
      </div>
      {copyMessage && <p className="referral-copy-success" role="status"><Check size={15} />{copyMessage}</p>}
      <FormError>{copyError}</FormError>
    </section>
    <section className="panel padded referral-rules">
      <h2>{t("返利规则")}</h2>
      <div className="referral-rates">{data.rates.map((rate, index) => <div key={index}><span>{levelNames()[index]}{t("好友充值")}</span><strong>{rate}<small>%</small></strong><p>{index === 0 ? t("你直接邀请的好友") : index === 1 ? t("一级好友邀请的好友") : t("二级好友邀请的好友")} · {number(data.level_counts[index])} {t(" 人")}</p></div>)}</div>
      <p className="muted">{config?.metered_billing ? t("好友充值到账后按实付本金计算返利，返利加入可消费余额，可用于用量扣费。") : t("邀请关系长期有效。好友每次充值到账后，按实付充值本金计算返利，自动加入你的可消费余额；充值赠送额度不参与返利。返利额度长期有效，可用于会员购买和下载扣费。")}</p>
      <div className="notice compact">{t("例如：一级好友充值 $100，你获得 $")}{rechargeMoney(100000000 * data.rates[0] / 100)}{t("；二级或三级好友充值 $100，你分别获得 $")}{rechargeMoney(100000000 * data.rates[1] / 100)}、${rechargeMoney(100000000 * data.rates[2] / 100)} {t(" 额度。")}</div>
    </section>
    {error && <ErrorState error={error} retry={() => refetch()} />}
    <section className="panel referral-records" aria-labelledby="referral-rewards-heading" aria-busy={isFetching}>
      <div className="panel-heading"><div><h2 id="referral-rewards-heading">{t("返利明细")}</h2><p>{t("每一笔好友充值带来的返利，都有记录。")}</p></div></div>
      {!data.rewards.data.length ? <Empty title={t("还没有返利记录")} detail={t("分享邀请链接，好友充值到账后即可获得返利额度。")} /> : <div className="table-wrap"><table><thead><tr><th>{t("好友")}</th><th>{t("邀请层级")}</th><th>{t("充值本金")}</th><th>{t("返利额度")}</th><th>{t("到账时间")}</th></tr></thead><tbody>{data.rewards.data.map(reward => <tr key={reward.id}><td>{reward.source_name}</td><td><Badge tone="violet">{levelNames()[reward.level - 1]}{t("好友")}</Badge></td><td>$ {rechargeMoney(reward.recharge_micros)}</td><td className="success-text">+ $ {rechargeMoney(reward.amount_micros)}</td><td className="muted">{date(reward.created_at, true)}</td></tr>)}</tbody></table></div>}
      <fieldset disabled={isFetching} className="referral-pager"><Pager page={rewardsPage} total={data.rewards.total} setPage={setRewardsPage} size={pageSize} /></fieldset>
    </section>
    <section className="panel referral-records" aria-labelledby="referral-members-heading" aria-busy={isFetching}>
      <div className="panel-heading"><div><h2 id="referral-members-heading">{t("我的团队")}</h2><p>{t("查看三级邀请关系中的好友。")}</p></div></div>
      {!data.members.data.length ? <Empty title={t("还没有邀请好友")} detail={t("复制你的专属邀请链接，邀请好友加入。")} /> : <div className="table-wrap"><table><thead><tr><th>{t("好友")}</th><th>{t("邀请层级")}</th><th>{t("注册时间")}</th></tr></thead><tbody>{data.members.data.map(member => <tr key={member.id}><td>{member.name}</td><td><Badge tone="violet">{levelNames()[member.level - 1]}{t("好友")}</Badge></td><td className="muted">{date(member.created_at, true)}</td></tr>)}</tbody></table></div>}
      <fieldset disabled={isFetching} className="referral-pager"><Pager page={membersPage} total={data.members.total} setPage={setMembersPage} size={pageSize} /></fieldset>
    </section>
  </div>;
}
