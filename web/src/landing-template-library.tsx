// i18n: static UI literals
import { useEffect, useRef, useState, type FormEvent } from 'react';
import { Link } from 'react-router-dom';
import { usePermissions } from 'react-admin';
import { Bookmark, Check, Code2, ExternalLink, Eye, FileCode2, LayoutTemplate, LoaderCircle, Pencil, Plus, RefreshCw, Search, Trash2 } from 'lucide-react';
import { api, send } from './api';
import { t, useLocale } from './i18n';
import { Badge, date, Empty, ErrorState, errorText, Field, FormError, Modal, PageHeader, Pager, Spinner } from './ui';
import { DIYLandingPreview } from './diy-landing-preview';
import { normalizeDIYLandingOptions, type DIYLandingKind, type DIYLandingOptions, type DIYLandingReference } from './diy-landing-types';
import './landing-template-library.css';

export type LandingLibraryTemplate = {
  id: string; name: string; description: string; cover_url?: string; cover_status?: 'pending' | 'processing' | 'ready' | 'failed'; cover_error?: string; config: DIYLandingOptions;
  version: number; enabled: boolean; favorite?: boolean; favorite_reference?: DIYLandingReference; favorite_version?: number; source_available?: boolean;
  html?: string; created_at?: string; updated_at?: string;
};
type TemplateList = { data: LandingLibraryTemplate[]; total: number };
type TemplateDraft = { name: string; description: string; html: string; config: DIYLandingOptions; enabled: boolean };
const templateDraft = (item?: LandingLibraryTemplate): TemplateDraft => ({ name: item?.name || '', description: item?.description || '', html: item?.html || '', config: normalizeDIYLandingOptions(item?.config), enabled: item?.enabled ?? true });
const templateKinds = (item: LandingLibraryTemplate) => (item.config?.targets || ['apk', 'pwa']).map(kind => kind.toUpperCase()).join(' / ');

const coverState = (item: LandingLibraryTemplate) => item.cover_status || (item.cover_url ? 'ready' : 'pending');
const pendingCover = (item: LandingLibraryTemplate) => ['pending', 'processing'].includes(coverState(item));
const coverStatusLabel = (item: LandingLibraryTemplate) => coverState(item) === 'ready' ? t('封面已生成') : coverState(item) === 'failed' ? t('封面生成失败') : t('封面生成中');

function useCoverPolling(items: LandingLibraryTemplate[], refresh: () => void, enabled = true) {
  const callback = useRef(refresh);
  callback.current = refresh;
  const pending = items.some(pendingCover);
  useEffect(() => {
    if (!enabled || !pending) return;
    let timer: number | undefined;
    const clear = () => { window.clearInterval(timer); timer = undefined; };
    const schedule = () => {
      clear();
      if (document.visibilityState !== 'hidden') timer = window.setInterval(() => callback.current(), 3000);
    };
    const visibility = () => {
      schedule();
      if (document.visibilityState !== 'hidden') callback.current();
    };
    schedule();
    document.addEventListener('visibilitychange', visibility);
    return () => { clear(); document.removeEventListener('visibilitychange', visibility); };
  }, [pending, enabled]);
}

function TemplateArtwork({ item }: { item: LandingLibraryTemplate }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [item.cover_url, item.cover_status]);
  const ready = coverState(item) === 'ready' && item.cover_url && !failed;
  const generating = !failed && pendingCover(item);
  return <div className="landing-library-artwork" aria-busy={generating}>{ready ? <img src={item.cover_url} alt="" loading="lazy" referrerPolicy="no-referrer" onError={() => setFailed(true)}/> : <div className={`landing-library-cover-state ${generating ? 'pending' : 'failed'}`} role="status">{generating ? <LoaderCircle size={22} className="spin"/> : <Eye size={22} strokeWidth={1.5}/>}<strong>{generating ? t('封面生成中') : failed ? t('封面加载失败') : t('封面生成失败')}</strong><p>{generating ? t('正在生成落地页的真实预览') : t('点击预览查看落地页真实效果')}</p></div>}<span className="landing-library-artwork-label"><FileCode2 size={13}/>{templateKinds(item)}</span></div>;
}

function TemplatePreview({ item, admin = false, snapshot = false, close, favorite, busy }: { item: LandingLibraryTemplate; admin?: boolean; snapshot?: boolean; close: () => void; favorite?: () => void; busy?: boolean }) {
  const [detail, setDetail] = useState<LandingLibraryTemplate>(), [error, setError] = useState('');
  const [mode, setMode] = useState<DIYLandingKind>(item.config?.targets?.includes('apk') ? 'apk' : 'pwa');
  useEffect(() => {
    const controller = new AbortController();
    setDetail(undefined); setError('');
    if (snapshot && item.favorite_reference) setDetail(item);
    else api<LandingLibraryTemplate>(`${admin ? '/admin' : ''}/landing-templates/${encodeURIComponent(item.id)}`, { signal: controller.signal }).then(setDetail).catch(failure => { if (!controller.signal.aborted) setError(errorText(failure)); });
    return () => controller.abort();
  }, [item.id, item.favorite_reference?.revision_id, admin, snapshot]);
  return <Modal wide title={item.name} close={close}><div className="landing-library-preview-head"><div><p>{item.description}</p><span>{templateKinds(item)} <small>v{item.version}</small></span></div><div className="row-actions"><div className="diy-segment" aria-label={t('模拟应用')}>{(item.config?.targets || ['apk', 'pwa']).map(kind => <button type="button" key={kind} aria-pressed={mode === kind} onClick={() => setMode(kind)}>{kind.toUpperCase()}</button>)}</div>{favorite && <button type="button" className={`btn ${item.favorite ? 'secondary' : 'primary'}`} disabled={busy} onClick={favorite}>{busy ? <LoaderCircle className="spin" size={16}/> : <Bookmark size={16} fill={item.favorite ? 'currentColor' : 'none'}/ >}{item.favorite ? t('取消收藏') : t('收藏模版')}</button>}</div></div><FormError>{error}</FormError>{detail ? <DIYLandingPreview html={snapshot ? undefined : detail.html} reference={snapshot ? item.favorite_reference : undefined} config={detail.config} mode={mode}/> : !error && <Spinner/>}<p className="landing-library-footnote">{t('收藏后可在APK详情页和PWA落地页中选择使用，已有应用使用的版本不会随模版更新而改变。')}</p></Modal>;
}

export function LandingTemplateLibraryPage() {
  useLocale();
  const [listing, setListing] = useState<TemplateList>({ data: [], total: 0 }), [loading, setLoading] = useState(true), [error, setError] = useState('');
  const [query, setQuery] = useState(''), [search, setSearch] = useState(''), [target, setTarget] = useState(''), [favorites, setFavorites] = useState(false), [page, setPage] = useState(1), [refresh, setRefresh] = useState(0);
  const [preview, setPreview] = useState<LandingLibraryTemplate>(), [mutating, setMutating] = useState(''), [message, setMessage] = useState('');
  const [coverRefresh, setCoverRefresh] = useState(0), requestBusy = useRef(false), loadedKey = useRef('');
  useCoverPolling(listing.data, () => { if (!requestBusy.current) setCoverRefresh(value => value + 1); });
  useEffect(() => {
    const controller = new AbortController(), key = JSON.stringify([page, search, target, favorites]);
    requestBusy.current = true;
    if (loadedKey.current !== key) setLoading(true);
    setError('');
    const params = new URLSearchParams({ page: String(page), per_page: '12', q: search, ...(target ? { target } : {}),  });
    api<TemplateList>(`${favorites ? '/landing-favorites' : '/landing-templates'}?${params}`, { signal: controller.signal }).then(result => { if (!controller.signal.aborted) { loadedKey.current = key; setListing(result); } }).catch(failure => { if (!controller.signal.aborted) setError(errorText(failure)); }).finally(() => { if (!controller.signal.aborted) { requestBusy.current = false; setLoading(false); } });
    return () => { controller.abort(); requestBusy.current = false; };
  }, [page, search, target, favorites, refresh, coverRefresh]);
  const toggleFavorite = async (item: LandingLibraryTemplate) => {
    if (mutating) return;
    setMutating(item.id); setError(''); setMessage('');
    try {
      let reference: DIYLandingReference | undefined;
      if (item.favorite) await api(`/landing-templates/${item.id}/favorite`, { method: 'DELETE' });
      else { const result = await send<{ reference: DIYLandingReference }>(`/landing-templates/${item.id}/favorite`, {}); reference = result.reference; }
      const next = { ...item, favorite: !item.favorite, favorite_reference: reference };
      setListing(current => ({ ...current, data: current.data.map(row => row.id === item.id ? next : row) }));
      if (preview?.id === item.id) setPreview(favorites && item.favorite ? undefined : next);
      setMessage(item.favorite ? t('已取消收藏，已绑定应用继续使用原版本。') : t('已收藏，可在APK和PWA中选择使用。'));
      setRefresh(value => value + 1);
    } catch (failure) { setError(errorText(failure)); } finally { setMutating(''); }
  };
  return <div className="landing-library-page"><PageHeader title={t('模版库')} subtitle={t('选择平台制作的落地页，收藏后即可用于你的应用。')} action={<Link to="/diy-landings" target="_blank" rel="noopener noreferrer" className="btn secondary"><Code2 size={16}/>{t('打开DIY落地页')}<ExternalLink size={13}/></Link>}/>
    <section className="landing-library-toolbar panel"><div className="landing-library-view-tabs" aria-label={t('模版范围')}><button type="button" aria-pressed={!favorites} onClick={() => { setFavorites(false); setPage(1); }}>{t('全部模版')}</button><button type="button" aria-pressed={favorites} onClick={() => { setFavorites(true); setPage(1); }}><Bookmark size={15}/>{t('我的收藏')}</button></div><div className="landing-library-filters"><select aria-label={t('适用应用')} value={target} onChange={event => { setTarget(event.target.value); setPage(1); }}><option value="">{t('全部应用类型')}</option><option value="apk">APK</option><option value="pwa">PWA</option></select><form className="search-box" onSubmit={event => { event.preventDefault(); setSearch(query); setPage(1); }}><Search size={16}/><input aria-label={t('搜索模版')} value={query} onChange={event => setQuery(event.target.value)} placeholder={t('搜索模版名称')}/><button type="submit" className="sr-only">{t('搜索')}</button></form></div></section>
    <FormError>{error}</FormError>{message && <p className="landing-library-feedback" role="status"><Check size={16}/>{message}</p>}
    {loading ? <Spinner/> : !listing.data.length ? <section className="panel"><Empty title={favorites ? t('还没有收藏的模版') : search ? t('没有找到匹配的模版') : t('模版库正在准备中')} detail={favorites ? t('在全部模版中预览并收藏，之后即可在应用中直接选择。') : search ? t('换个名称或应用类型试试。') : t('管理员上架模版后，会显示在这里。')} action={favorites && <button type="button" className="btn secondary" onClick={() => { setFavorites(false); setSearch(''); setQuery(''); setPage(1); }}>{t('浏览全部模版')}</button>}/></section> : <div className="landing-library-grid">{listing.data.map(item => <article key={item.id} className="landing-library-card"><button type="button" className="landing-library-preview-link" aria-label={t('预览模版 {0}', { 0: item.name })} onClick={() => setPreview(item)}><TemplateArtwork item={item}/><span className="landing-library-preview-affordance"><Eye size={16}/>{t('预览效果')}</span></button><div className="landing-library-card-body"><div className="landing-library-card-title"><h2>{item.name}</h2><small>v{item.version}</small></div><p>{item.description || t('收藏这个落地页，开始配置你的应用。')}</p>{item.source_available === false && <small className="landing-library-retained">{t('原模版已下架，收藏版本仍可使用。')}</small>}<footer><span>{item.favorite ? <><Check size={14}/>{t('已收藏')}</> : t('平台精选')}</span><button type="button" className={`btn ${item.favorite ? 'secondary' : 'primary'}`} disabled={Boolean(mutating)} onClick={() => void toggleFavorite(item)}>{mutating === item.id ? <LoaderCircle size={15} className="spin"/> : <Bookmark size={15} fill={item.favorite ? 'currentColor' : 'none'}/ >}{item.favorite ? t('取消收藏') : t('收藏模版')}</button></footer></div></article>)}</div>}
    {!loading && listing.total > 0 && <Pager size={12} page={page} total={listing.total} setPage={setPage}/>}
    {preview && <TemplatePreview snapshot={favorites} item={preview} close={() => setPreview(undefined)} favorite={() => void toggleFavorite(preview)} busy={Boolean(mutating)}/>}
  </div>;
}

function LibraryTemplateEditor({ initial, close, saved }: { initial?: LandingLibraryTemplate; close: () => void; saved: () => void }) {
  const [draft, setDraft] = useState<TemplateDraft>(() => templateDraft(initial));
  const original = useRef(JSON.stringify(templateDraft(initial)));
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [mode, setMode] = useState<DIYLandingKind>('apk');
  const name = useRef<HTMLInputElement>(null);
  const update = <K extends keyof TemplateDraft>(key: K, value: TemplateDraft[K]) => setDraft(current => ({ ...current, [key]: value }));
  const options = (values: Partial<DIYLandingOptions>) => update('config', { ...draft.config, ...values });
  const dismiss = () => { if (!busy && (JSON.stringify(draft) === original.current || window.confirm(t('模版尚未保存，确定关闭吗？')))) close(); };
  const submit = async (event: FormEvent) => {
    event.preventDefault(); if (busy) return;
    if (!draft.name.trim()) { setError(t('请填写模版名称')); name.current?.focus(); return; }
    if (!draft.html.trim()) { setError(t('请粘贴模版HTML代码')); return; }
    if (!draft.config.targets.length) { setError(t('请选择至少一种适用应用')); return; }
    setBusy(true); setError('');
    try { await send(`/admin/landing-templates${initial ? `/${initial.id}` : ''}`, { ...draft, ...(initial ? { expected_version: initial.version } : {}) }, initial ? 'PUT' : 'POST'); saved(); }
    catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  return <Modal wide title={initial ? t('编辑模版') : t('添加模版')} close={dismiss}><form noValidate className="landing-library-editor" onSubmit={submit}><div className="landing-library-editor-metadata"><Field label={t('模版名称')}><input ref={name} aria-label={t('模版名称')} required maxLength={100} value={draft.name} disabled={busy} onChange={event => update('name', event.target.value)} placeholder={t('例如：简洁应用安装页')}/></Field><Field label={t('模版说明')}><input aria-label={t('模版说明')} maxLength={500} value={draft.description} disabled={busy} onChange={event => update('description', event.target.value)} placeholder={t('说明适用场景与页面特点')}/></Field></div><p className="landing-library-auto-cover"><Eye size={16}/>{t('保存后自动生成落地页真实封面，无需上传图片。')}</p><div className="landing-library-editor-options"><div className="landing-library-targets"><strong>{t('适用应用')}</strong>{(['apk', 'pwa'] as const).map(kind => <label key={kind}><input type="checkbox" checked={draft.config.targets.includes(kind)} disabled={busy} onChange={event => options({ targets: event.target.checked ? [...draft.config.targets, kind] : draft.config.targets.filter(value => value !== kind) })}/>{kind.toUpperCase()}</label>)}</div><label><input type="checkbox" checked={draft.enabled} disabled={busy} onChange={event => update('enabled', event.target.checked)}/><span>{t('上架到模版库')}<small>{t('上架后，客户可以预览和收藏。')}</small></span></label><label><input type="checkbox" checked={draft.config.platform_button} disabled={busy} onChange={event => options({ platform_button: event.target.checked })}/><span>{t('显示平台主按钮')}</span></label></div><div className="landing-library-editor-settings"><Field label={t('主按钮选择器（选填）')}><input value={draft.config.primary_selector} disabled={busy} onChange={event => options({ primary_selector: event.target.value })} placeholder="#install-button"/></Field><Field label={t('资源基址（选填）')}><input type="url" value={draft.config.resource_base} disabled={busy} onChange={event => options({ resource_base: event.target.value })} placeholder="https://example.com/assets/"/></Field></div><div className="landing-library-code-grid"><Field label={t('模版HTML代码')}><textarea aria-label={t('模版HTML代码')} required rows={20} spellCheck={false} wrap="off" value={draft.html} disabled={busy} onChange={event => update('html', event.target.value)} placeholder={'<!doctype html>\n<html>\n  <body>\n    <button data-d1-action="primary">安装应用</button>\n  </body>\n</html>'}/></Field><div><div className="landing-library-editor-preview-mode"><span>{t('模拟应用')}</span><div className="diy-segment"><button type="button" aria-pressed={mode === 'apk'} onClick={() => setMode('apk')}>APK</button><button type="button" aria-pressed={mode === 'pwa'} onClick={() => setMode('pwa')}>PWA</button></div></div><DIYLandingPreview html={draft.html} config={draft.config} mode={mode} compact/></div></div><FormError>{error}</FormError><div className="landing-library-editor-footer"><p>{t('客户收藏的是固定版本，编辑模版不会改动已收藏和已绑定的应用。')}</p><div className="row-actions"><button type="button" className="btn secondary" disabled={busy} onClick={dismiss}>{t('取消')}</button><button className="btn primary" disabled={busy}>{busy ? <LoaderCircle size={16} className="spin"/> : <Check size={16}/ >}{busy ? t('保存中…') : draft.enabled ? t('保存并上架') : t('保存模版')}</button></div></div></form></Modal>;
}

export function AdminLandingTemplateLibraryPage() {
  useLocale();
  const { permissions, isPending } = usePermissions();
  const [listing, setListing] = useState<TemplateList>({ data: [], total: 0 }), [loading, setLoading] = useState(true), [error, setError] = useState('');
  const [query, setQuery] = useState(''), [search, setSearch] = useState(''), [page, setPage] = useState(1), [refresh, setRefresh] = useState(0);
  const [editor, setEditor] = useState<{ item?: LandingLibraryTemplate }>(), [preview, setPreview] = useState<LandingLibraryTemplate>(), [deleting, setDeleting] = useState<LandingLibraryTemplate>(), [busy, setBusy] = useState(false), [message, setMessage] = useState('');
  const [coverRefresh, setCoverRefresh] = useState(0), requestBusy = useRef(false), loadedKey = useRef('');
  useCoverPolling(listing.data, () => { if (!requestBusy.current) setCoverRefresh(value => value + 1); }, permissions === 'admin');
  useEffect(() => {
    if (permissions !== 'admin') return;
    const controller = new AbortController(), key = JSON.stringify([page, search]);
    requestBusy.current = true;
    if (loadedKey.current !== key) setLoading(true);
    setError('');
    api<TemplateList>(`/admin/landing-templates?page=${page}&per_page=20&q=${encodeURIComponent(search)}`, { signal: controller.signal }).then(result => { if (!controller.signal.aborted) { loadedKey.current = key; setListing(result); } }).catch(failure => { if (!controller.signal.aborted) setError(errorText(failure)); }).finally(() => { if (!controller.signal.aborted) { requestBusy.current = false; setLoading(false); } });
    return () => { controller.abort(); requestBusy.current = false; };
  }, [permissions, page, search, refresh, coverRefresh]);
  const edit = async (item: LandingLibraryTemplate) => {
    if (busy) return; setBusy(true); setError('');
    try { const detail = await api<LandingLibraryTemplate>(`/admin/landing-templates/${item.id}`); setEditor({ item: detail }); }
    catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  const toggle = async (item: LandingLibraryTemplate) => {
    if (busy) return; setBusy(true); setError(''); setMessage('');
    try { const detail = await api<LandingLibraryTemplate>(`/admin/landing-templates/${item.id}`); await send(`/admin/landing-templates/${item.id}`, { ...templateDraft(detail), enabled: !item.enabled, expected_version: detail.version }, 'PUT'); setRefresh(value => value + 1); setMessage(item.enabled ? t('模版已下架，已收藏的版本继续有效。') : t('模版已上架。')); }
    catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  const retryCover = async (item: LandingLibraryTemplate) => {
    if (busy) return; setBusy(true); setError(''); setMessage('');
    try {
      await send(`/admin/landing-templates/${item.id}/cover/retry`, {});
      setListing(current => ({ ...current, data: current.data.map(row => row.id === item.id ? { ...row, cover_status: 'pending', cover_error: '' } : row) }));
      setRefresh(value => value + 1); setMessage(t('已开始重新生成预览封面。'));
    } catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  const remove = async () => {
    if (!deleting || busy) return; setBusy(true); setError('');
    try { await api(`/admin/landing-templates/${deleting.id}`, { method: 'DELETE' }); setDeleting(undefined); setRefresh(value => value + 1); setMessage(t('模版已删除，客户已收藏的版本继续有效。')); }
    catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  if (isPending) return <Spinner/>;
  if (permissions !== 'admin') return <Empty title={t('仅管理员可以管理模版库')}/>;
  return <div className="landing-library-page"><PageHeader title={t('模版库管理')} subtitle={t('提前制作落地页，上架后供客户收藏使用。')} action={<button type="button" className="btn primary" onClick={() => setEditor({})}><Plus size={17}/>{t('添加模版')}</button>}/><section className="panel"><div className="table-toolbar"><div className="table-title">{t('全部模版')} <span>{listing.total}</span></div><form className="search-box" onSubmit={event => { event.preventDefault(); setSearch(query); setPage(1); }}><Search size={16}/><input aria-label={t('搜索模版')} placeholder={t('搜索模版名称')} value={query} onChange={event => setQuery(event.target.value)}/><button type="submit" className="sr-only">{t('搜索')}</button></form></div><FormError>{error}</FormError>{message && <p className="landing-library-feedback" role="status"><Check size={16}/>{message}</p>}{loading ? <Spinner/> : !listing.data.length ? <Empty title={t('还没有模版')} detail={t('添加并上架第一个落地页，让客户直接收藏使用。')} action={<button type="button" className="btn secondary" onClick={() => setEditor({})}><Plus size={16}/>{t('添加模版')}</button>}/> : <div className="table-wrap"><table><thead><tr><th>{t('模版')}</th><th>{t('适用应用')}</th><th>{t('状态')}</th><th>{t('更新时间')}</th><th className="right">{t('操作')}</th></tr></thead><tbody>{listing.data.map(item => <tr key={item.id}><td><div className="landing-library-admin-name"><LayoutTemplate size={22}/><div><strong>{item.name}</strong><small>{item.description || `v${item.version}`}</small></div></div></td><td>{templateKinds(item)}</td><td><Badge tone={item.enabled ? 'green' : 'gray'}>{item.enabled ? t('已上架') : t('未上架')}</Badge><span className={`landing-library-cover-status ${coverState(item)}`} title={item.cover_error}>{pendingCover(item) && <LoaderCircle size={12} className="spin"/>}{coverStatusLabel(item)}</span></td><td>{date(item.updated_at, true)}</td><td><div className="row-actions wrap"><button type="button" className="text-btn" onClick={() => setPreview(item)}><Eye size={14}/>{t('预览')}</button>{coverState(item) === 'failed' && <button type="button" className="text-btn" disabled={busy} onClick={() => void retryCover(item)}><RefreshCw size={14}/>{t('重新生成封面')}</button>}<button type="button" className="text-btn" disabled={busy} onClick={() => void edit(item)}><Pencil size={14}/>{t('编辑')}</button><button type="button" className="text-btn" disabled={busy} onClick={() => void toggle(item)}>{item.enabled ? t('下架') : t('上架')}</button><button type="button" className="text-btn danger-text" disabled={busy} onClick={() => setDeleting(item)}><Trash2 size={14}/>{t('删除')}</button></div></td></tr>)}</tbody></table></div>}<Pager page={page} total={listing.total} setPage={setPage}/></section>
    {editor && <LibraryTemplateEditor initial={editor.item} close={() => setEditor(undefined)} saved={() => { setEditor(undefined); setRefresh(value => value + 1); setMessage(t('模版已保存，预览封面会自动更新。')); }}/>}
    {preview && <TemplatePreview admin item={preview} close={() => setPreview(undefined)}/>}
    {deleting && <Modal title={t('删除模版？')} close={() => { if (!busy) setDeleting(undefined); }}><p className="modal-intro">{deleting.name}</p><p>{t('删除后不再显示在模版库中，客户已经收藏和绑定的版本继续有效。')}</p><FormError>{error}</FormError><div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={() => setDeleting(undefined)}>{t('取消')}</button><button type="button" className="btn danger" disabled={busy} onClick={() => void remove()}>{busy ? t('正在删除…') : t('确认删除')}</button></div></Modal>}
  </div>;
}
