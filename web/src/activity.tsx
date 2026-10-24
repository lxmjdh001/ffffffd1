import { useRef, useState, type FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Ban, Search } from 'lucide-react';
import { api, send } from './api';
import type { Download, Page } from './types';
import { Badge, bytes, date, Empty, errorText, ErrorState, Field, FormError, Modal, money, PageHeader, Pager, Spinner } from './ui';
import './activity.css';

type DateRange = { start: string; end: string };
type FilterApp = { id: string; name: string; deleted: boolean };
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
  const [appID, setAppID] = useState('');
  const [draftAppID, setDraftAppID] = useState('');
  const [formError, setFormError] = useState('');
  const [uaAction, setUAAction] = useState<{ download: Download; blocked: boolean }>();
  const [uaError, setUAError] = useState('');
  const [uaBusy, setUABusy] = useState(false);
  const uaSubmitting = useRef(false);
  const cache = useQueryClient();
  const { data: apps, error: appsError, refetch: refetchApps } = useQuery({
    queryKey: ['download-filter-apps'],
    queryFn: () => api<{ data: FilterApp[] }>('/downloads/apps'),
  });
  const { data, error, refetch, isFetching } = useQuery({
    queryKey: ['downloads', range.start, range.end, appID, page],
    queryFn: () => api<Page<Download>>(`/downloads?${new URLSearchParams({ page: String(page), start_date: range.start, end_date: range.end, ...(appID ? { app_id: appID } : {}) })}`),
  });
  const applyRange = (next: DateRange) => {
    const start = Date.parse(`${next.start}T00:00:00Z`), end = Date.parse(`${next.end}T00:00:00Z`);
    if (!Number.isFinite(start) || !Number.isFinite(end)) { setFormError('请选择开始日期和结束日期'); return; }
    if (end < start) { setFormError('结束日期不能早于开始日期'); return; }
    if (next.end > todayInShanghai()) { setFormError('只能查询今天及以前的下载记录'); return; }
    if (end - start >= 30 * dayMillis) { setFormError('单次最多查询 30 天下载记录（含起止日期）'); return; }
    setFormError(''); setDraft(next); setPage(1); setRange(next); setAppID(draftAppID);
    if (page === 1 && sameRange(next, range) && appID === draftAppID) void refetch();
  };
  const submit = (event: FormEvent) => { event.preventDefault(); applyRange(draft); };
  const saveUABlock = async () => {
    if (!uaAction || uaSubmitting.current) return;
    uaSubmitting.current = true; setUABusy(true); setUAError('');
    try {
      const result = await send<{ blocked: boolean }>(`/downloads/${encodeURIComponent(uaAction.download.id)}/ua-block`, { blocked: uaAction.blocked }, 'PUT');
      cache.setQueriesData<Page<Download>>({ queryKey: ['downloads'] }, current => current ? { ...current, data: current.data.map(download => download.user_agent === uaAction.download.user_agent ? { ...download, ua_blocked: result.blocked } : download) } : current);
      setUAAction(undefined);
      await cache.invalidateQueries({ queryKey: ['downloads'] });
    } catch (error) { setUAError(errorText(error)); }
    finally { uaSubmitting.current = false; setUABusy(false); }
  };
  const isToday = sameRange(range, presetRange(1)) && !appID;
  const selectedApp = apps?.data.find(app => app.id === appID);

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
          <Field label="应用"><select className="download-app-filter" value={draftAppID} disabled={!apps && !appsError} onChange={e => setDraftAppID(e.target.value)}>
            <option value="">全部应用</option>
            {apps?.data.map(app => <option key={app.id} value={app.id}>{app.name || app.id}{app.deleted ? '（已删除）' : ''}</option>)}
          </select></Field>
          <Field label="开始日期"><input type="date" required max={todayInShanghai()} value={draft.start} onChange={e => { setDraft({ ...draft, start: e.target.value }); setFormError(''); }} /></Field>
          <Field label="结束日期"><input type="date" required max={todayInShanghai()} value={draft.end} onChange={e => { setDraft({ ...draft, end: e.target.value }); setFormError(''); }} /></Field>
          <button className="btn primary" disabled={isFetching}><Search size={16} />{isFetching ? '查询中…' : '查询'}</button>
        </div>
        <FormError>{formError}</FormError>
        {appsError && <div className="download-app-error" role="status"><span>应用列表加载失败，仍可查询全部记录。</span><button type="button" className="text-btn" onClick={() => void refetchApps()}>重试应用列表</button></div>}
      </form>
      <div className="panel-heading"><div><h2>{isToday ? '今日下载' : '筛选结果'}</h2><p>{range.start === range.end ? range.start : `${range.start} 至 ${range.end}`}{selectedApp ? ` · ${selectedApp.name || selectedApp.id}${selectedApp.deleted ? '（已删除）' : ''}` : ''}</p></div><span className="muted small">只展示完整传输成功的下载，分段自动合并</span></div>
      {error ? <ErrorState error={error} retry={() => refetch()} /> : !data ? <Spinner /> : !data.data.length ? <Empty title="所选条件暂无下载记录" detail="可以切换应用或日期范围查看其他下载记录。" /> : <div className="table-wrap"><table><thead><tr><th>应用名称</th><th>IP / 设备</th><th>来源</th><th>计费方式</th><th>本次费用</th><th>完成时间 / 用时</th></tr></thead><tbody>{data.data.map(d => <tr key={d.id}><td className="semibold">{d.app_name}</td><td><strong className="mono small">{d.ip}</strong><small className="cell-sub">{d.device} · {d.os} · {d.browser}</small><details className="ua-detail"><summary>查看 UA</summary><p className="download-ua-value">{d.user_agent || '此记录未提供 UA'}</p><div className="download-ua-actions">{d.ua_blocked && <Badge tone="red">已拉黑</Badge>}<button type="button" className={`text-btn ${d.ua_blocked ? '' : 'danger-text'}`} disabled={uaBusy || !d.user_agent.trim()} onClick={() => { setUAAction({ download: d, blocked: !d.ua_blocked }); setUAError(''); }}><Ban size={13} />{d.ua_blocked ? '解除拉黑' : '拉黑 UA'}</button></div>{!d.user_agent.trim() && <small className="download-ua-empty">无法拉黑未提供 UA 的记录。</small>}</details></td><td className="source-cell" title={d.source}>{d.source || "直接访问 / 未提供"}</td><td><Badge tone={d.amount_micros > 0 ? 'amber' : 'green'}>{d.amount_micros > 0 ? '余额扣费' : '免费额度 / 零单价'}</Badge></td><td>$ {money(d.amount_micros, 6)}</td><td>{date(d.completed_at, true)}<small className="cell-sub">{bytes(d.bytes)} · {Math.max(0, (new Date(d.completed_at).getTime() - new Date(d.started_at).getTime()) / 1000).toFixed(1)} 秒</small></td></tr>)}</tbody></table></div>}
      <Pager page={page} total={data?.total || 0} setPage={setPage} />
    </section>
    {uaAction && <Modal title={uaAction.blocked ? '拉黑此 UA？' : '解除此 UA 的拉黑？'} close={() => { if (!uaBusy) setUAAction(undefined); }}>
      <p className="download-ua-confirm">{uaAction.blocked ? '拉黑后，使用此 UA 的访问将无法进入你名下的下载、分流及斗篷入口，并显示默认 404 页面。此操作仅对你的账户生效，其他用户不受影响。' : '解除后，此 UA 可以重新访问你名下的下载、分流及斗篷入口，仍须符合入口的其他访问规则。'}</p>
      <p className="download-ua-preview">{uaAction.download.user_agent}</p>
      <FormError>{uaError}</FormError>
      <div className="modal-footer"><button type="button" className="btn secondary" disabled={uaBusy} onClick={() => setUAAction(undefined)}>取消</button><button type="button" className={`btn ${uaAction.blocked ? 'danger' : 'primary'}`} disabled={uaBusy} onClick={() => void saveUABlock()}>{uaBusy ? '处理中…' : uaAction.blocked ? '确认拉黑' : '确认解除'}</button></div>
    </Modal>}
    <p className="bottom-hint">开始下载时预留额度或余额，服务端完整传输文件后才结算。断点续传合并字节范围，下载凭证过期仍未完成则释放预留。成功表示服务端完整传输，不代表客户端安装或注册成功。</p>
  </>;
}
