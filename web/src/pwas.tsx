import { AppConfigImport } from './app-config-import';
import { resetFieldErrors, clearFieldError, firstInvalid, focusField, focusSaveError } from './form-validation';
import { pwaLocaleIssue } from './creation-validation';
import { VisitStatisticsButton } from './visit-statistics';
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { QRCodeSVG } from 'qrcode.react';
import { ArrowDownToLine, ArrowUpRight, Bell, Check, Copy, Globe2, ImagePlus, Pencil, Plus, QrCode, Smartphone, Trash2, Users } from 'lucide-react';
import { api, newKey, send } from './api';
import { t, useLocale } from './i18n';
import { Badge, date, Empty, ErrorState, errorText, Field, FormError, Modal, number, PageHeader, Spinner } from './ui';
import type { PWAApp, PWAInput } from './pwa-types';
import { ReturnFlowEditor, normalizeReturnFlow, validReturnFlow } from './return-flow';
import { defaultPWALanding, normalizePWALanding, PWALandingEditor, PWALandingPreview } from './pwa-landing';
import { DomainShareDrawer } from './domain-share-drawer';
import './pwas.css';

const blank = (): PWAInput => ({ name: '', target_url: '', description: '', theme_color: '#6d78de', enabled: true, push_enabled: true, landing_config: defaultPWALanding(), return_flow_config: normalizeReturnFlow() });
const editable = (app: PWAInput): PWAInput => ({ name: app.name, target_url: app.target_url, description: app.description, theme_color: app.theme_color, enabled: app.enabled, push_enabled: app.push_enabled, landing_config: app.landing_config?.template ? normalizePWALanding(app.landing_config) : undefined, return_flow_config: normalizeReturnFlow(app.return_flow_config) });
export const validPWAURL = (value: string) => { try { const url = new URL(value); return (url.protocol === 'https:' || (url.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname))) && !!url.hostname && !url.username && !url.password && (!url.port || url.port === '443' || url.protocol === 'http:') && new TextEncoder().encode(value).length <= 2048; } catch { return false; } };
export const validPWAIcon = (file: File) => ['image/png', 'image/jpeg'].includes(file.type) && file.size > 0 && file.size <= 2 * 1024 * 1024;

export function PWAIcon({ app, className = '' }: { app: Pick<PWAApp, 'name' | 'icon_url'> & { updated_at?: string }; className?: string }) {
  const icon = app.icon_url && app.updated_at ? `${app.icon_url}${app.icon_url.includes('?') ? '&' : '?'}v=${encodeURIComponent(app.updated_at)}` : app.icon_url;
  return <span className={`pwa-icon ${className}`}>{icon ? <img src={icon} alt="" /> : <span>{app.name.trim().slice(0, 1) || 'P'}</span>}</span>;
}

export function PWAEditor({ initial, close }: { initial?: PWAApp; close: () => void }) {
  const [activeLanguage,setActiveLanguage]=useState(initial?.landing_config?.storeInfos?.[0]?.language||'en-US');
  const [draft, setDraft] = useState<PWAInput>(() => initial ? editable(initial) : blank());
  const [file, setFile] = useState<File>();
  const [preview, setPreview] = useState(initial?.icon_url || '');
  const [saved, setSaved] = useState<PWAApp | undefined>(initial);
  const [error, setError] = useState('');
  const [iconError, setIconError] = useState('');
  const [busy, setBusy] = useState(false);
 const [mediaBusy,setMediaBusy]=useState(false);
  const [imported, setImported] = useState<PWAApp>();
  const formRef = useRef<HTMLFormElement>(null);
  const applyImport = async (id: string) => { resetFieldErrors(formRef.current); const source = await api<PWAApp>(`/pwas/${id}`); setDraft(structuredClone(editable(source))); setImported(source); setFile(undefined); setIconError(''); setError(''); setActiveLanguage(source.landing_config?.storeInfos?.[0]?.language || 'en-US'); requestKey.current = newKey(); };
  const submitting = useRef(false);
  const requestKey = useRef(newKey());
  const cache = useQueryClient();
  useEffect(() => {
    if (!file) { setPreview(saved?.icon_url || imported?.icon_url || ''); return; }
    const reader = new FileReader(); let current = true;
    reader.onload = () => { if (current && typeof reader.result === 'string') setPreview(reader.result); };
    reader.onerror = () => { if (current) setPreview(saved?.icon_url || ''); };
    reader.readAsDataURL(file);
    return () => { current = false; reader.onload = null; reader.onerror = null; if (reader.readyState === FileReader.LOADING) reader.abort(); };
  }, [file, saved?.icon_url, imported?.icon_url]);
  const update = <K extends keyof PWAInput>(key: K, value: PWAInput[K]) => setDraft(current => ({ ...current, [key]: value }));
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (submitting.current || mediaBusy) return;
    if (firstInvalid(formRef.current)) return;
    const issue = !validPWAURL(draft.target_url.trim()) ? ['target_url',t('请输入有效的 HTTPS 网页 URL')] : !/^#[0-9a-f]{6}$/i.test(draft.theme_color) ? ['theme_color',t('请选择有效模板和十六进制主题颜色')] : iconError ? ['icon',iconError] : undefined;
    if (issue) { focusField(formRef.current?.querySelector(`[data-field="${issue[0]}"]`),issue[1]); return; }
    const localeIssue = pwaLocaleIssue(draft.landing_config);
    if (localeIssue) { setActiveLanguage(localeIssue.language); setError(localeIssue.message); requestAnimationFrame(() => focusField(formRef.current?.querySelector(localeIssue.selector),localeIssue.message)); return; }
    if (!validReturnFlow(draft.return_flow_config)) { focusField(formRef.current?.querySelector('.return-flow-editor input')); return; }
    submitting.current = true; setBusy(true); setError('');
    try {
      const landing=normalizePWALanding(draft.landing_config);if(landing.template!=='diy')landing.storeInfos=landing.storeInfos.map(v=>({...v,appName:v.appName.trim()||draft.name.trim()}));
      const payload = { ...draft, import_from: saved ? undefined : imported?.id, landing_config:draft.landing_config ? landing : undefined, name: draft.name.trim(), description: draft.description.trim(), target_url: draft.target_url.trim() };
      let result = await send<PWAApp>(saved ? `/pwas/${saved.id}` : '/pwas', payload, saved ? 'PUT' : 'POST', saved ? undefined : requestKey.current);
      setSaved(result); cache.setQueryData(['pwa',result.id],result); await cache.invalidateQueries({ queryKey: ['pwas'] });
      if (file) { const data = new FormData(); data.set('file', file); result = await api<PWAApp>(`/pwas/${result.id}/icon`, { method: 'POST', body: data }); setSaved(result); }
      cache.setQueryData(['pwa',result.id],result); await cache.invalidateQueries({ queryKey: ['pwas'] }); close();
    } catch (failure) { setError(errorText(failure)); focusSaveError(formRef.current,errorText(failure)); }
    finally { submitting.current = false; setBusy(false); }
  };
  const selectIcon = (next?: File) => { if (!next) return; if (!validPWAIcon(next)) { setIconError(t('请选择不超过 2 MB 的 PNG 或 JPG 图片。')); return; } setIconError(''); setFile(next); };
  return <div className="pwa-creation-page"><PageHeader eyebrow="WEB TO APP" title={initial ? t('编辑 PWA 应用') : t('新建 PWA 应用')} subtitle={t('配置应用资料与落地页，预览后保存。')} action={<>{!initial && !saved && <AppConfigImport kind="pwas" disabled={busy||mediaBusy} apply={applyImport}/>}<button type="button" className="btn secondary" disabled={busy||mediaBusy} onClick={close}>{t('返回应用列表')}</button></>}/><form ref={formRef} noValidate onInput={clearFieldError} onChange={clearFieldError} aria-label={initial ? t('编辑 PWA 应用') : t('新建 PWA 应用')} onSubmit={submit} className="pwa-editor pwa-page-editor"><div className="pwa-editor-fields"><div className="pwa-form-section"><span className="pwa-section-number">01</span><div><h3>{t('应用资料')}</h3><p>{t('让你的网页，拥有自己的桌面入口。')}</p></div></div><Field label={t('应用名称')}><input data-field="name" autoFocus required maxLength={100} value={draft.name} disabled={busy} onChange={event => update('name', event.target.value)} placeholder={t('例如：我的在线商店')} /></Field><Field label={t('网页 URL')} hint={t('填写用户打开应用后访问的网页地址。')}><input data-field="target_url" aria-label={t('网页 URL')} type="url" required maxLength={2048} value={draft.target_url} disabled={busy} onChange={event => update('target_url', event.target.value)} placeholder="https://example.com" /></Field><Field label={t('应用简介')}><textarea rows={3} maxLength={500} value={draft.description} disabled={busy} onChange={event => update('description', event.target.value)} placeholder={t('简单介绍你的应用，可留空。')} /></Field><div className="pwa-icon-field"><PWAIcon app={{ name: draft.name, icon_url: preview }} /><div><strong>{t('应用图标')}</strong><label className={`btn secondary pwa-upload ${busy ? 'disabled' : ''}`}><ImagePlus size={15} />{file || preview ? t('更换图标') : t('上传图标')}<input data-field="icon" aria-label={t('上传应用图标')} type="file" accept="image/png,image/jpeg" disabled={busy} onChange={event => selectIcon(event.target.files?.[0])} /></label><small>{t('PNG / JPG，最大 2 MB，建议使用正方形图片。')}</small><FormError>{iconError}</FormError></div></div><Field label={t('主题颜色')}><div className="pwa-color-field"><input type="color" aria-label={t('选择主题颜色')} value={draft.theme_color} disabled={busy} onChange={event => update('theme_color', event.target.value)} /><input data-field="theme_color" aria-label={t('主题颜色值')} maxLength={7} value={draft.theme_color} disabled={busy} onChange={event => update('theme_color', event.target.value)} /></div></Field><div className="pwa-settings"><label><input type="checkbox" checked={draft.enabled} disabled={busy} onChange={event => update('enabled', event.target.checked)} /><span>{t('启用应用')}<small>{t('关闭后，安装入口和应用将暂停访问。')}</small></span></label><label><input type="checkbox" checked={draft.push_enabled} disabled={busy} onChange={event => update('push_enabled', event.target.checked)} /><span>{t('开启通知订阅')}<small>{t('用户允许通知后，可接收你发送的消息。')}</small></span></label></div><PWALandingEditor value={normalizePWALanding(draft.landing_config)} change={value=>update('landing_config',value)} disabled={busy||mediaBusy} uploading={setMediaBusy} activeLanguage={activeLanguage} selectLanguage={language=>{resetFieldErrors(formRef.current);setActiveLanguage(language);}}/><ReturnFlowEditor value={draft.return_flow_config} change={value => update('return_flow_config',value)} disabled={busy} uploading={setMediaBusy} /></div><div className="pwa-preview-column"><PWALandingPreview draft={draft} icon={preview} language={activeLanguage}/></div><div className="pwa-editor-footer">{saved && !initial && <p className="notice compact" role="status">{t('应用已创建。若图标上传失败，可直接重试保存。')}</p>}<FormError>{error}</FormError><div className="modal-footer"><button className="btn secondary" type="button" disabled={busy || mediaBusy} onClick={close}>{t('取消')}</button><button className="btn primary" disabled={busy || mediaBusy}>{busy ? t('正在保存…') : initial || saved ? t('保存应用') : t('创建应用')}</button></div></div></form></div>;
}

export function PWAsPage() {
  useLocale();
  const { data, error, refetch } = useQuery({ queryKey: ['pwas'], queryFn: () => api<{ data: PWAApp[] }>('/pwas') });
  const navigate=useNavigate();
  const [qr, setQR] = useState<PWAApp>();
  const [sharing, setSharing] = useState<PWAApp>();
  const [deleting, setDeleting] = useState<PWAApp>();
  const [busy, setBusy] = useState(false);
  const submitting = useRef(false);
  const [message, setMessage] = useState('');
  const cache = useQueryClient();
  const mutate = async (app: PWAApp, remove = false) => {
    if (submitting.current) return; submitting.current = true; setBusy(true); setMessage('');
    try { if (remove) await api(`/pwas/${app.id}`, { method: 'DELETE' }); else await send(`/pwas/${app.id}`, { ...editable(app), enabled: !app.enabled }, 'PUT'); setDeleting(undefined); await Promise.all([refetch(), cache.invalidateQueries({ queryKey: ['pwa-push'] })]); }
    catch (failure) { setMessage(errorText(failure)); }
    finally { submitting.current = false; setBusy(false); }
  };
  return <div className="pwa-page"><PageHeader eyebrow="WEB TO APP" title={t('PWA管理')} subtitle={t('把网页变成可安装的应用，让用户从桌面再次找到你。')} action={<button className="btn primary" onClick={() => navigate('/pwas/new')}><Plus size={16} />{t('新建 PWA 应用')}</button>} /><div className="pwa-overview"><div className="pwa-overview-intro"><span><Smartphone size={25} /></span><div><h2>{t('一个网页，一个专属应用。')}</h2><p>{t('配置网址、名称和图标，即可分享安装入口。')}</p></div></div><div className="pwa-overview-metrics"><div><strong>{number(data?.data.length || 0)}</strong><span>{t('PWA 应用')}</span></div><div><strong>{number(data?.data.reduce((total, app) => total + app.subscription_count, 0) || 0)}</strong><span>{t('通知订阅')}</span></div></div></div>{!deleting && <FormError>{message}</FormError>}{error ? <ErrorState error={error} retry={() => refetch()} /> : !data ? <Spinner /> : !data.data.length ? <section className="panel"><Empty title={t('让网页，走进用户的桌面')} detail={t('创建你的第一个 PWA 应用，分享安装链接，开始建立长期连接。')} action={<button className="btn secondary" onClick={() => navigate('/pwas/new')}><Plus size={15} />{t('创建第一个 PWA')}</button>} /></section> : <div className="pwa-card-grid">{data.data.map(app => <article className="panel pwa-app-card" key={app.id}><header><PWAIcon app={app} /><div><h2>{app.name}</h2><span>{date(app.updated_at)} {t('更新')}</span></div><Badge tone={app.enabled ? 'blue' : 'gray'}>{app.enabled ? t('已启用') : t('已暂停')}</Badge></header><p className="pwa-app-description">{app.description || t('连接网页与桌面，让访问更轻松。')}</p><div className="pwa-card-url"><Globe2 size={14} /><span>{app.target_url}</span><a href={app.target_url} target="_blank" rel="noreferrer" className="icon-btn" aria-label={t('打开目标网页')}><ArrowUpRight size={15} /></a></div><div className="pwa-app-stats"><span><Users size={14} />{t('订阅人数')}<strong>{number(app.subscription_count)}</strong></span><span><Bell size={14} />{app.push_enabled ? t('通知已开启') : t('通知已关闭')}</span></div><div className="pwa-entry"><span>{t('安装链接')}</span><div><a href={app.entry_url} target="_blank" rel="noreferrer">{app.entry_url}</a><button type="button" className="text-btn" onClick={() => setSharing(app)}><Copy size={14}/>{t('复制')}</button><button type="button" className="icon-btn" aria-label={t('安装二维码')} onClick={() => setQR(app)}><QrCode size={17} /></button></div></div><footer><div><VisitStatisticsButton kind="pwas" id={app.id} name={app.name}/><button className="text-btn" disabled={busy} onClick={() => navigate(`/pwas/${app.id}/edit`)}><Pencil size={13} />{t('编辑')}</button><button className="text-btn" disabled={busy} onClick={() => mutate(app)}>{app.enabled ? t('暂停应用') : t('启用应用')}</button></div><div><Link className="text-btn" to={`/pwa-push?app=${encodeURIComponent(app.id)}`}><Bell size={13} />{t('发送通知')}</Link><button className="icon-btn danger-text" disabled={busy} aria-label={t('删除应用')} onClick={() => { setMessage(''); setDeleting(app); }}><Trash2 size={15} /></button></div></footer></article>)}</div>}<p className="pwa-bottom-note"><Bell size={14} />{t('消息通知需要用户主动授权；不同设备的安装与通知支持可能有所不同。')}</p>{sharing && <DomainShareDrawer path={`/p/${sharing.slug}/`} close={() => setSharing(undefined)} />}{qr && <Modal title={t('安装二维码')} close={() => setQR(undefined)}><div className="pwa-qr"><PWAIcon app={qr} /><h3>{qr.name}</h3><QRCodeSVG value={qr.entry_url} size={200} level="M" title={t('扫描二维码安装应用')} /><p>{t('使用手机浏览器打开，按页面指引安装。')}</p><a href={qr.entry_url} target="_blank" rel="noreferrer">{qr.entry_url}</a><button type="button" className="btn secondary" onClick={() => { setSharing(qr); setQR(undefined); }}><Copy size={14}/>{t('复制安装链接')}</button></div></Modal>}{deleting && <Modal title={t('删除 PWA 应用？')} close={() => { if (!submitting.current) setDeleting(undefined); }}><p className="modal-intro">{deleting.name}</p><p>{t('删除后，安装入口、已有应用访问和通知推送将停止。此操作无法撤销。')}</p><FormError>{message}</FormError><div className="modal-footer"><button className="btn secondary" disabled={busy} onClick={() => setDeleting(undefined)}>{t('取消')}</button><button className="btn danger" disabled={busy} onClick={() => mutate(deleting, true)}>{busy ? t('正在删除…') : t('确认删除应用')}</button></div></Modal>}</div>;
}

export function PWAEditorPage() {
 useLocale();const {id}=useParams();const navigate=useNavigate();
 const query=useQuery({queryKey:['pwa',id],queryFn:()=>api<PWAApp>(`/pwas/${id}`),enabled:!!id,refetchOnMount:'always',refetchOnWindowFocus:false,refetchOnReconnect:false});
 if(id&&query.error)return <ErrorState error={query.error} retry={()=>query.refetch()}/>;
 if(id&&(!query.data||query.isFetching))return <Spinner/>;
 return <PWAEditor key={id||'new'} initial={query.data} close={()=>navigate('/pwas')}/>;
}
