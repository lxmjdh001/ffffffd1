import { useState, type FormEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Search } from 'lucide-react';
import { api } from './api';
import type { Download, Page } from './types';
import { Badge, bytes, date, Empty, ErrorState, Field, FormError, money, PageHeader, Pager, Spinner } from './ui';

type DateRange = { start: string; end: string };
const dayMillis = 86400000;
// Match the server's UTC+8 calendar, independent of the visitor's timezone.
const todayInShanghai = () => new Date(Date.now() + 8 * 3600000).toISOString().slice(0, 10);
const shiftDate = (day: string, offset: number) => new Date(Date.parse(`${day}T00:00:00Z`) + offset * dayMillis).toISOString().slice(0, 10);
const sameRange = (a: DateRange, b: DateRange) => a.start === b.start && a.end === b.end;
function presetRange(days: number): DateRange {
  const end = days === 0 ? shiftDate(todayInShanghai(), -1) : todayInShanghai();
  return { start: shiftDate(end, -(Math.max(1, days) - 1)), end };
}

export function ActivityPage() {
  const [page, setPage] = useState(1);
  const [range, setRange] = useState<DateRange>(() => presetRange(1));
  const [draft, setDraft] = useState<DateRange>(range);
  const [formError, setFormError] = useState('');
  const { data, error, refetch, isFetching } = useQuery({
    queryKey: ['downloads', range.start, range.end, page],
    queryFn: () => api<Page<Download>>(`/downloads?${new URLSearchParams({ page: String(page), start_date: range.start, end_date: range.end })}`),
  });
  const applyRange = (next: DateRange) => {
    const start = Date.parse(`${next.start}T00:00:00Z`), end = Date.parse(`${next.end}T00:00:00Z`);
    if (!Number.isFinite(start) || !Number.isFinite(end)) { setFormError('请选择开始日期和结束日期'); return; }
    if (end < start) { setFormError('结束日期不能早于开始日期'); return; }
    if (next.end > todayInShanghai()) { setFormError('只能查询今天及以前的下载记录'); return; }
    if (end - start >= 30 * dayMillis) { setFormError('单次最多查询 30 天下载记录（含起止日期）'); return; }
    setFormError(''); setDraft(next); setPage(1); setRange(next);
    if (page === 1 && sameRange(next, range)) void refetch();
  };
  const submit = (event: FormEvent) => { event.preventDefault(); applyRange(draft); };
  const isToday = sameRange(range, presetRange(1));

  return <>
    <PageHeader eyebrow="DOWNLOAD ACTIVITY" title="下载记录" subtitle="完整下载的设备、来源与费用，一目了然。" />
    <section className="panel">
      <form className="download-filters" onSubmit={submit} noValidate aria-label="下载记录筛选">
        <div className="download-presets" aria-label="快捷日期">
          {[[1, '今天'], [0, '昨天'], [7, '最近 7 天'], [30, '最近 30 天']].map(([days, label]) => {
            const next = presetRange(Number(days));
            return <button type="button" key={days} className={sameRange(range, next) ? 'selected' : ''} aria-pressed={sameRange(range, next)} onClick={() => applyRange(next)}>{label}</button>;
          })}
          <span className="muted small">按北京时间统计 · 单次最多 30 天</span>
        </div>
        <div className="download-date-fields">
          <Field label="开始日期"><input type="date" required max={todayInShanghai()} value={draft.start} onChange={e => { setDraft({ ...draft, start: e.target.value }); setFormError(''); }} /></Field>
          <Field label="结束日期"><input type="date" required max={todayInShanghai()} value={draft.end} onChange={e => { setDraft({ ...draft, end: e.target.value }); setFormError(''); }} /></Field>
          <button className="btn primary" disabled={isFetching}><Search size={16} />{isFetching ? '查询中…' : '查询'}</button>
        </div>
        <FormError>{formError}</FormError>
      </form>
      <div className="panel-heading"><div><h2>{isToday ? '今日下载' : '筛选结果'}</h2><p>{range.start === range.end ? range.start : `${range.start} 至 ${range.end}`}</p></div><span className="muted small">只展示完整传输成功的下载，分段自动合并</span></div>
      {error ? <ErrorState error={error} retry={() => refetch()} /> : !data ? <Spinner /> : !data.data.length ? <Empty title="所选日期暂无下载记录" detail="可以切换日期范围查看其他下载记录。" /> : <div className="table-wrap"><table><thead><tr><th>应用名称</th><th>IP / 设备</th><th>来源</th><th>计费方式</th><th>本次费用</th><th>完成时间 / 用时</th></tr></thead><tbody>{data.data.map(d => <tr key={d.id}><td className="semibold">{d.app_name}</td><td><strong className="mono small">{d.ip}</strong><small className="cell-sub">{d.device} · {d.os} · {d.browser}</small><details className="ua-detail"><summary>查看 UA</summary>{d.user_agent}</details></td><td className="source-cell" title={d.source}>{d.source || "直接访问 / 未提供"}</td><td><Badge tone={d.amount_micros > 0 ? 'amber' : 'green'}>{d.amount_micros > 0 ? '余额扣费' : '免费额度 / 零单价'}</Badge></td><td>$ {money(d.amount_micros, 6)}</td><td>{date(d.completed_at, true)}<small className="cell-sub">{bytes(d.bytes)} · {Math.max(0, (new Date(d.completed_at).getTime() - new Date(d.started_at).getTime()) / 1000).toFixed(1)} 秒</small></td></tr>)}</tbody></table></div>}
      <Pager page={page} total={data?.total || 0} setPage={setPage} />
    </section>
    <p className="bottom-hint">开始下载时预留额度或余额，服务端完整传输文件后才结算。断点续传合并字节范围，下载凭证过期仍未完成则释放预留。成功表示服务端完整传输，不代表客户端安装或注册成功。</p>
  </>;
}
