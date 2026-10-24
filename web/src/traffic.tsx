// i18n: static UI literals
import { t, useLocale } from './i18n';
import { useState, type FormEvent } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { Copy, Plus } from 'lucide-react';
import { api, send } from './api';
import { DomainShareDrawer } from './domain-share-drawer';
import { Badge, date, Empty, ErrorState, errorText, Field, FormError, Modal, PageHeader, Spinner } from './ui';

type TrafficLink = { id: string; url: string; status: string; error: string; checked_at: string | null; position: number };
type TrafficItem = { pool: { name: string; mode: 'random' | 'sequential'; enabled: boolean; risk_enabled: boolean; slug: string }; links: TrafficLink[]; entry_url: string };
type TrafficResponse = { paid: boolean; configured: boolean; data: TrafficItem[] };
type Draft = { slug?: string; identifier: string; name: string; mode: 'random' | 'sequential'; text: string; enabled: boolean; risk_enabled: boolean };
const emptyDraft = (): Draft => ({ identifier: '', name: '', mode: 'random', text: '', enabled: true, risk_enabled: false });
function isEligible(l: TrafficLink, item: TrafficItem, paid: boolean) {
 if (!item.pool.enabled) return false;
 if (!item.pool.risk_enabled || !paid) return true;
 const host = new URL(l.url).hostname.toLowerCase();
 const blocked = item.links.some(x => new URL(x.url).hostname.toLowerCase() === host && x.status === 'unsafe');
 return !blocked && item.pool.enabled && paid && l.status === 'clean' && !!l.checked_at && new Date(l.checked_at).getTime() > Date.now() - 120 * 60 * 1000;
}
export function TrafficPage() {
  useLocale();
 const { data, error, refetch } = useQuery({ queryKey: ['traffic-pools'], queryFn: () => api<TrafficResponse>('/traffic-pools'), refetchInterval: 15000 });
 const [draft, setDraft] = useState<Draft>(); const [details, setDetails] = useState<string>(); const [deleting, setDeleting] = useState<TrafficItem>();
 const [busy, setBusy] = useState(false); const [message, setMessage] = useState('');
 const [sharing, setSharing] = useState<TrafficItem>();
 const refresh = async () => { await refetch(); };
 const edit = (item: TrafficItem) => { setMessage(''); setDraft({ slug: item.pool.slug, identifier: item.pool.slug, name: item.pool.name, mode: item.pool.mode || 'random', enabled: item.pool.enabled, risk_enabled: item.pool.risk_enabled && data?.paid === true, text: item.links.map(l => l.url).join('\n') }); };
 const save = async (e: FormEvent) => {
  e.preventDefault(); if (!draft) return; setBusy(true); setMessage('');
  try { await send(draft.slug ? '/traffic-pools/' + draft.slug : '/traffic-pools', { slug: draft.identifier.trim() || undefined, name: draft.name, mode: draft.mode, enabled: draft.enabled, risk_enabled: draft.risk_enabled, urls: draft.text.split(/\r?\n/).map(s => s.trim()).filter(Boolean) }, draft.slug ? 'PUT' : 'POST'); await refresh(); setDraft(undefined); }
  catch (e) { setMessage(errorText(e)); } finally { setBusy(false); }
 };
 const toggle = async (item: TrafficItem) => { setBusy(true); setMessage(''); try { await send('/traffic-pools/' + item.pool.slug, { name: item.pool.name, mode: item.pool.mode, enabled: !item.pool.enabled, risk_enabled: item.pool.risk_enabled && data?.paid === true, urls: item.links.map(l => l.url) }, 'PUT'); await refresh(); } catch (e) { setMessage(errorText(e)); } finally { setBusy(false); } };
 const remove = async () => { if (!deleting) return; setBusy(true); setMessage(''); try { await api('/traffic-pools/' + deleting.pool.slug, { method: 'DELETE' }); await refresh(); setDeleting(undefined); } catch (e) { setMessage(errorText(e)); } finally { setBusy(false); } };
 if (error) return <ErrorState error={error} retry={refresh} />;
 if (!data) return <Spinner />;
 const selected = data.data.find(x => x.pool.slug === details);
 return <><PageHeader eyebrow="TRAFFIC ROUTING" title={t("应用分流")} subtitle={t("创建应用分流入口，支持随机或顺序展示。")} action={<button className="btn primary" onClick={() => { setMessage(''); setDraft(emptyDraft()); }}><Plus size={16} />{t("新建分流")}</button>} />
 {!data.paid && <div className="notice">{t("应用分流免费使用，Google 安全检测为付费会员功能。")}<Link to="/billing">{t("查看会员方案")}</Link></div>}
 {!data.configured && <div className="notice">{t("Google 安全检测暂未配置，普通分流可正常使用。")}</div>}
 {!draft && !deleting && <FormError>{message}</FormError>}
 <section className="panel"><div className="panel-heading"><h2>{t("分流列表")}</h2><span className="muted small">{t("共 ")}{data.data.length} {t(" 个分流")}</span></div>
 {!data.data.length ? <Empty title={t("暂无分流")} detail={t("点击新建分流，在弹窗中输入下载链接，每行一条。")} action={<button className="btn secondary" onClick={() => { setMessage(''); setDraft(emptyDraft()); }}><Plus size={15} />{t("新建分流")}</button>} /> : <div className="table-wrap"><table><thead><tr><th>{t("分流名称 / 入口")}</th><th>{t("展示方式")}</th><th>{t("链接数 / 可用")}</th><th>{t("状态")}</th><th>{t("Google 安全检测")}</th><th className="right">{t("操作")}</th></tr></thead><tbody>{data.data.map(item => <tr key={item.pool.slug}><td><strong>{item.pool.name}</strong><small className="cell-sub mono" style={{ overflowWrap: 'anywhere' }}>{item.entry_url || t("暂无可用域名")}</small><button type="button" className="copy-btn" onClick={() => setSharing(item)}><Copy size={14} />{t("复制入口")}</button></td><td>{item.pool.mode === 'sequential' ? t("顺序展示") : t("随机展示")}</td><td>{item.links.length} / {item.links.filter(l => isEligible(l, item, data.paid)).length}</td><td><Badge tone={item.pool.enabled ? 'green' : 'gray'}>{item.pool.enabled ? t("已开启") : t("已暂停")}</Badge></td><td>{item.pool.risk_enabled && data.paid ? t("已开启") : item.pool.risk_enabled ? t("会员到期，已停用") : t("未开启")}</td><td><div className="row-actions wrap"><button className="text-btn" onClick={() => setDetails(item.pool.slug)}>{t("风险检测")}</button><button className="text-btn" onClick={() => edit(item)}>{t("编辑")}</button><button className="text-btn" disabled={busy} onClick={() => toggle(item)}>{item.pool.enabled ? t("关闭") : t("开启")}</button><button className="text-btn danger-text" onClick={() => { setMessage(''); setDeleting(item); }}>{t("删除")}</button></div></td></tr>)}</tbody></table></div>}
 </section>
 {sharing && <DomainShareDrawer key={sharing.pool.slug} path={`/r/${sharing.pool.slug}`} title={t("选择域名复制分流入口")} close={() => setSharing(undefined)} />}
 {draft && <Modal title={draft.slug ? t("编辑分流") : t("新建分流")} close={() => { if (!busy) setDraft(undefined); }} wide><form onSubmit={save}>
 <Field label={t("分流名称")}><input maxLength={60} placeholder={t("例如：应用下载分流")} value={draft.name} onChange={e => setDraft({ ...draft, name: e.target.value })} /></Field>
 <Field label={t("分流标识")} hint={draft.slug ? t("可修改，1–32 位字母、数字、短横线或下划线；修改后旧入口失效。") : t("留空自动生成 8 位随机标识，也可手动填写，全平台不可重复。")}><input required={!!draft.slug} maxLength={32} pattern="[a-zA-Z0-9][a-zA-Z0-9_-]{0,31}" value={draft.identifier} placeholder={t("例如：my-app 或留空随机生成")} onChange={e => setDraft({ ...draft, identifier: e.target.value.toLowerCase() })} /></Field>
 {draft.slug && draft.identifier !== draft.slug && <p className="small muted">{t("保存后入口改为 /r/")}{draft.identifier}{t("，原 /r/")}{draft.slug} {t(" 将失效。")}</p>}
 <Field label={t("APK 下载链接（一行一条）")} hint={t("支持 1–30 条完整 HTTP/HTTPS 下载链接，空行会自动忽略。")}><textarea required rows={8} placeholder={'https://a.example.com/app.apk\nhttps://b.example.com/app.apk\nhttps://c.example.com/app.apk'} value={draft.text} onChange={e => setDraft({ ...draft, text: e.target.value })} /></Field>
 <Field label={t("展示方式")} hint={draft.mode === 'sequential' ? t("按输入顺序逐次分配：1 → 2 → 3 → 1，自动跳过不可用链接。") : t("每次访问从可用链接中等概率随机选择一条。")}><select value={draft.mode} onChange={e => setDraft({ ...draft, mode: e.target.value as Draft['mode'] })}><option value="random">{t("随机展示")}</option><option value="sequential">{t("顺序展示")}</option></select></Field>
 <label className="switch-field"><input type="checkbox" checked={draft.enabled} onChange={e => setDraft({ ...draft, enabled: e.target.checked })} /><span>{t("启用")}<small>{t("所有用户均可使用，保存后按所选方式分配访问。")}</small></span></label>
 <label className="switch-field"><input type="checkbox" checked={draft.risk_enabled} disabled={!draft.risk_enabled && (!data.paid || !data.configured)} onChange={e => setDraft({ ...draft, risk_enabled: e.target.checked })} /><span>{t("分流安全检测")}<small>{t("如果域名红了则自动下架，不参与分流")}</small><small>{!data.paid ? t("付费会员可用，普通分流不受影响") : !data.configured ? t("等待管理员配置检测服务") : t("每 120 分钟自动检测；会员到期后恢复普通分流。")}</small></span></label>
 <FormError>{message}</FormError><div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={() => setDraft(undefined)}>{t("取消")}</button><button className="btn primary" disabled={busy}>{busy ? t("保存中…") : t("保存分流")}</button></div></form></Modal>}
 {selected && <Modal title={selected.pool.name + t(" · 风险检测")} close={() => setDetails(undefined)} wide><p className="modal-intro">{t("平台定时检测域名是否风险，风险域名将停止参与分流")}</p><div className="table-wrap"><table><thead><tr><th>{t("下载链接")}</th><th>{t("风险状态")}</th><th>{t("参与分流")}</th><th>{t("最近检测")}</th></tr></thead><tbody>{selected.links.map(l => <tr key={l.id}><td style={{ overflowWrap: 'anywhere', maxWidth: 260 }}>{l.url}{l.error && <small className="cell-sub">{t(l.error)}</small>}</td><td><Badge tone={l.status === 'unsafe' ? 'red' : l.status === 'clean' ? 'green' : 'gray'}>{({ unsafe: t("命中风险"), clean: t("未命中风险"), unknown: t("检测失败") } as Record<string, string>)[l.status] || (selected.pool.risk_enabled && data.paid ? t("等待检测") : t("未开启检测"))}</Badge></td><td>{isEligible(l, selected, data.paid) ? t("参与") : t("不参与")}</td><td>{date(l.checked_at, true)}</td></tr>)}</tbody></table></div></Modal>}
 {deleting && <Modal title={t("删除分流？")} close={() => { if (!busy) setDeleting(undefined); }}><p>{t("删除「")}{deleting.pool.name}{t("」后，此分流入口将停止访问。")}</p><FormError>{message}</FormError><div className="modal-footer"><button className="btn secondary" disabled={busy} onClick={() => setDeleting(undefined)}>{t("取消")}</button><button className="btn danger" disabled={busy} onClick={remove}>{t("删除分流")}</button></div></Modal>}
 </>;
}
