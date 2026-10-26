// i18n: static UI literals
import { useCallback, useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from 'react';
import { Link, useNavigate, useParams } from 'react-router-dom';
import { Archive, Check, Code2, Copy, FileCode2, FolderOpen, History, LoaderCircle, Plus, Save, Search, Settings2, Trash2, Users } from 'lucide-react';
import { api, newKey, send } from './api';
import { t, useLocale } from './i18n';
import { Badge, bytes, date, Empty, errorText, Field, FormError, Modal, Pager, Spinner } from './ui';
import { DIYLandingPreview } from './diy-landing-preview';
import { defaultDIYLandingOptions, normalizeDIYLandingOptions, type DIYHTMLIssue, type DIYLandingBinding, type DIYLandingKind, type DIYLandingOptions, type DIYLandingPage, type DIYLandingPageSummary, type DIYLandingRevision } from './diy-landing-types';
import './diy-landings.css';

const exampleHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>应用安装页</title>
  <style>
    body { margin:0; padding:64px 28px; background:#f1f7fa; color:#24374b; font-family:system-ui,sans-serif; }
    main { max-width:440px; margin:auto; }
    h1 { font-size:36px; line-height:1.25; }
    p { line-height:1.8; color:#5c7084; }
    button { margin-top:24px; padding:16px 28px; border:0; border-radius:10px; background:#167d9a; color:white; font-size:16px; }
  </style>
</head>
<body>
  <main>
    <h1>让精彩随时发生</h1>
    <p>在这里介绍你的应用，以及安装后可以体验的内容。</p>
    <button type="button" data-d1-action="primary"><span data-d1-bind="primary-label">立即安装</span></button>
  </main>
</body>
</html>`;
const fingerprint = (name: string, html: string, config: DIYLandingOptions) => JSON.stringify({ name, html, config });
type PageList = { data: DIYLandingPageSummary[]; total: number };

export function DIYLandingsPage() {
  useLocale();
  const { id } = useParams(); const navigate = useNavigate();
  const [item, setItem] = useState<DIYLandingPage>(), [loading, setLoading] = useState(Boolean(id));
  const [name, setName] = useState(''), [html, setHTML] = useState(''), [config, setConfig] = useState<DIYLandingOptions>(defaultDIYLandingOptions);
  const [saved, setSaved] = useState(fingerprint('', '', defaultDIYLandingOptions()));
  const [busy, setBusy] = useState(false), [error, setError] = useState(''), [status, setStatus] = useState('');
  const [showPages, setShowPages] = useState(false), [settings, setSettings] = useState(false), [showVersions, setShowVersions] = useState(false);
  const [mode, setMode] = useState<DIYLandingKind>('apk'), [issues, setIssues] = useState<DIYHTMLIssue[]>([]), [previewTick, setPreviewTick] = useState(0);
  const [mobileTab, setMobileTab] = useState<'code' | 'preview'>('code'), [split, setSplit] = useState(50);
  const [bindings, setBindings] = useState<{ page: DIYLandingPageSummary; data?: DIYLandingBinding[]; error?: string }>();
  const [deletePage, setDeletePage] = useState<DIYLandingPageSummary>();
  const [listing, setListing] = useState<PageList>({ data: [], total: 0 }), [listBusy, setListBusy] = useState(false), [listError, setListError] = useState('');
  const [page, setPage] = useState(1), [search, setSearch] = useState(''), [query, setQuery] = useState(''), [listVersion, setListVersion] = useState(0);
  const source = useRef<HTMLTextAreaElement>(null), nameInput = useRef<HTMLInputElement>(null), grid = useRef<HTMLDivElement>(null);
  const loaded = useRef(''), publishKey = useRef('');
  const dirty = saved !== fingerprint(name, html, config);
  const archived = Boolean(item?.archived_at);
  const dirtyRef = useRef(dirty); dirtyRef.current = dirty;
  const lineCount = html.split('\n').length;
  const documentBytes = new TextEncoder().encode(html).length;
  const [scrollTop, setScrollTop] = useState(0);

  const applyPage = useCallback((next: DIYLandingPage) => {
    const options = normalizeDIYLandingOptions(next.draft_config);
    loaded.current = next.id; setItem(next); setName(next.name); setHTML(next.draft_html || ''); setConfig(options);
    setSaved(fingerprint(next.name, next.draft_html || '', options)); setError(''); setStatus(''); publishKey.current = '';
  }, []);

  useEffect(() => {
    if (!id) {
      loaded.current = ''; setItem(undefined); setName(''); setHTML(''); setConfig(defaultDIYLandingOptions());
      setSaved(fingerprint('', '', defaultDIYLandingOptions())); setLoading(false); setError(''); setStatus(''); return;
    }
    if (loaded.current === id) return;
    const controller = new AbortController(); loaded.current = ''; setItem(undefined); setName(''); setHTML(''); setConfig(defaultDIYLandingOptions());
    setSaved(fingerprint('', '', defaultDIYLandingOptions())); setLoading(true); setError(''); setStatus('');
    api<DIYLandingPage>(`/diy-landings/${encodeURIComponent(id)}`, { signal: controller.signal }).then(applyPage)
      .catch(failure => { if (!controller.signal.aborted) setError(errorText(failure)); }).finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [id, applyPage]);
  useEffect(() => {
    const warn = (event: BeforeUnloadEvent) => { if (dirtyRef.current) { event.preventDefault(); event.returnValue = ''; } };
    const guardNavigation = (event: MouseEvent) => {
      const anchor = (event.target as Element)?.closest?.('a');
      if (!dirtyRef.current || !anchor || anchor.target === '_blank' || anchor.origin !== location.origin) return;
      if (!window.confirm(t('当前代码尚未保存，确定离开吗？'))) { event.preventDefault(); event.stopPropagation(); }
    };
    window.addEventListener('beforeunload', warn); document.addEventListener('click', guardNavigation, true);
    return () => { window.removeEventListener('beforeunload', warn); document.removeEventListener('click', guardNavigation, true); };
  }, []);
  useEffect(() => {
    if (!showPages) return;
    const controller = new AbortController(); setListBusy(true); setListError('');
    api<PageList>(`/diy-landings?page=${page}&per_page=20&q=${encodeURIComponent(query)}`, { signal: controller.signal }).then(setListing)
      .catch(failure => { if (!controller.signal.aborted) setListError(errorText(failure)); }).finally(() => { if (!controller.signal.aborted) setListBusy(false); });
    return () => controller.abort();
  }, [showPages, page, query, listVersion]);

  const mayLeave = () => !dirty || window.confirm(t('当前代码尚未保存，确定离开吗？'));
  const openPage = (next: DIYLandingPageSummary) => {
    if (!mayLeave()) return;
    setShowPages(false); navigate(`/diy-landings/${next.id}/edit`);
    if (id === next.id) { setLoading(true); api<DIYLandingPage>(`/diy-landings/${next.id}`).then(applyPage).catch(failure => setError(errorText(failure))).finally(() => setLoading(false)); }
  };
  const newPage = () => {
    if (!mayLeave()) return;
    dirtyRef.current = false; loaded.current = '';
    setItem(undefined); setName(''); setHTML(''); setConfig(defaultDIYLandingOptions()); setSaved(fingerprint('', '', defaultDIYLandingOptions())); setStatus(''); setError('');
    navigate('/diy-landings');
  };
  const saveDraft = async (): Promise<DIYLandingPage> => {
    if (!name.trim()) { nameInput.current?.focus(); throw new Error(t('请填写落地页名称')); }
    if (documentBytes > 5 * 1024 * 1024) { source.current?.focus(); throw new Error(t('HTML代码不能超过 5 MiB')); }
    const next = await send<DIYLandingPage>(item ? `/diy-landings/${item.id}` : '/diy-landings', { name: name.trim(), html, config, ...(item ? { draft_version: item.draft_version } : {}) }, item ? 'PUT' : 'POST');
    applyPage(next); setListVersion(value => value + 1);
    if (!item) navigate(`/diy-landings/${next.id}/edit`, { replace: true });
    return next;
  };
  const save = async (publish = false) => {
    if (busy) return;
    setBusy(true); setError(''); setStatus('');
    try {
      const next = dirty || !item ? await saveDraft() : item;
      if (publish) {
        if (!html.trim()) { source.current?.focus(); throw new Error(t('请先粘贴页面HTML代码')); }
        publishKey.current ||= newKey();
        const published = await send<DIYLandingPage>(`/diy-landings/${next.id}/publish`, { draft_version: next.draft_version }, 'POST', publishKey.current);
        applyPage(published); setListVersion(value => value + 1); setStatus(t('已发布，可在APK或PWA创建和编辑时选择。'));
      } else setStatus(t('草稿已保存，正在使用的版本不会改变。'));
    } catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  const copyPage = async (entry: DIYLandingPageSummary) => {
    if (!mayLeave()) return;
    setBusy(true); setListError('');
    try { const next = await send<DIYLandingPage>(`/diy-landings/${entry.id}/copy`, {}); applyPage(next); navigate(`/diy-landings/${next.id}/edit`); setShowPages(false); setListVersion(value => value + 1); }
    catch (failure) { setListError(errorText(failure)); } finally { setBusy(false); }
  };
  const archivePage = async (entry: DIYLandingPageSummary) => {
    setBusy(true); setListError('');
    try { const next = await send<DIYLandingPage>(`/diy-landings/${entry.id}/archive`, { archived: !entry.archived_at }, 'PATCH'); if (item?.id === entry.id) setItem(current => current ? { ...current, archived_at: next.archived_at } : current); setListVersion(value => value + 1); }
    catch (failure) { setListError(errorText(failure)); } finally { setBusy(false); }
  };
  const viewBindings = async (entry: DIYLandingPageSummary) => {
    setBindings({ page: entry });
    try { const result = await api<{ data: DIYLandingBinding[] } | DIYLandingBinding[]>(`/diy-landings/${entry.id}/bindings`); setBindings({ page: entry, data: Array.isArray(result) ? result : result.data }); }
    catch (failure) { setBindings({ page: entry, error: errorText(failure) }); }
  };
  const removePage = async () => {
    if (!deletePage) return;
    setBusy(true); setListError('');
    try { await api(`/diy-landings/${deletePage.id}`, { method: 'DELETE' }); if (item?.id === deletePage.id) { dirtyRef.current = false; loaded.current = ''; setItem(undefined); setName(''); setHTML(''); setConfig(defaultDIYLandingOptions()); setSaved(fingerprint('', '', defaultDIYLandingOptions())); navigate('/diy-landings'); } setDeletePage(undefined); setListVersion(value => value + 1); }
    catch (failure) { setListError(errorText(failure)); setDeletePage(undefined); } finally { setBusy(false); }
  };
  const loadRevision = async (revision: DIYLandingRevision) => {
    if (!item || !mayLeave()) return;
    setBusy(true); setError('');
    try { const full = await api<DIYLandingRevision>(`/diy-landings/${item.id}/revisions/${revision.id}`); setHTML(full.raw_html || ''); setConfig(normalizeDIYLandingOptions(full.config)); setShowVersions(false); setStatus(t('已载入历史版本，保存或发布后生效。')); }
    catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  const deleteRevision = async (revision: DIYLandingRevision) => {
    if (!item || !window.confirm(t('确定删除这个未引用的历史版本吗？'))) return;
    setBusy(true); setError('');
    try { await api(`/diy-landings/${item.id}/revisions/${revision.id}`, { method: 'DELETE' }); const next = await api<DIYLandingPage>(`/diy-landings/${item.id}`); setItem(current => current ? { ...current, revisions: next.revisions } : current); }
    catch (failure) { setError(errorText(failure)); } finally { setBusy(false); }
  };
  const editKey = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key !== 'Tab' || event.shiftKey) return;
    event.preventDefault(); const field = event.currentTarget, start = field.selectionStart, end = field.selectionEnd;
    field.setRangeText('  ', start, end, 'end'); setHTML(field.value);
  };
  const moveDivider = (event: React.PointerEvent<HTMLDivElement>) => {
    event.currentTarget.setPointerCapture(event.pointerId);
    const move = (pointer: PointerEvent) => { const box = grid.current?.getBoundingClientRect(); if (box) setSplit(Math.max(30, Math.min(70, (pointer.clientX - box.left) / box.width * 100))); };
    const stop = () => { window.removeEventListener('pointermove', move); window.removeEventListener('pointerup', stop); };
    window.addEventListener('pointermove', move); window.addEventListener('pointerup', stop, { once: true });
  };
  const focusIssue = (issue: DIYHTMLIssue) => {
    if (!issue.line || !source.current) return;
    const field = source.current, offset = html.split('\n').slice(0, issue.line - 1).reduce((sum, line) => sum + line.length + 1, 0);
    setMobileTab('code'); field.focus(); field.setSelectionRange(offset, offset + (html.split('\n')[issue.line - 1]?.length || 0)); field.scrollTop = Math.max(0, issue.line - 4) * 21;
  };

  if (loading) return <Spinner/>;
  return <div className="diy-workbench">
    <div className="diy-workbench-heading"><div><h1>{t('DIY落地页')}</h1><p>{t('粘贴网页代码，制作可用于APK和PWA的安装页面。')}</p></div><div className="row-actions wrap"><button type="button" className="btn secondary" onClick={() => setShowPages(true)}><FolderOpen size={16}/>{t('我的落地页')}</button><button type="button" className="btn secondary" disabled={busy} onClick={newPage}><Plus size={16}/>{t('新建')}</button></div></div>
    <div className="diy-document-toolbar"><div className="diy-document-name"><label htmlFor="diy-page-name">{t('落地页名称')}</label><input ref={nameInput} id="diy-page-name" value={name} maxLength={100} disabled={busy || archived} onChange={event => setName(event.target.value)} placeholder={t('例如：春季活动安装页')}/><Badge tone={archived ? 'gray' : dirty ? 'amber' : item?.latest_published_revision_id ? 'green' : 'gray'}>{archived ? t('已归档') : dirty ? t('未保存') : item?.latest_published_revision_id ? t('已发布') : t('草稿')}</Badge></div><div className="row-actions"><button type="button" className="icon-btn" aria-label={t('历史版本')} disabled={!item || busy} onClick={() => setShowVersions(true)}><History size={18}/></button><button type="button" className="btn secondary" disabled={busy || archived} onClick={() => void save()}><Save size={15}/>{t('保存草稿')}</button><button type="button" className="btn primary" disabled={busy || archived} onClick={() => void save(true)}>{busy ? <LoaderCircle size={15} className="spin"/> : <Check size={15}/ >}{t('保存并发布')}</button></div></div>
    <FormError>{error}</FormError>{status && <p className="diy-feedback" role="status"><Check size={15}/>{status}</p>}{archived && <div className="notice compact">{t('此落地页已归档，当前应用可继续使用。恢复后才能编辑或新增绑定。')}</div>}
    <div className="diy-mobile-tabs" role="tablist" aria-label={t('工作台视图')}><button type="button" role="tab" aria-selected={mobileTab === 'code'} onClick={() => setMobileTab('code')}>{t('代码')}</button><button type="button" role="tab" aria-selected={mobileTab === 'preview'} onClick={() => setMobileTab('preview')}>{t('预览')}</button></div>
    <div ref={grid} className={`diy-editor-grid mobile-${mobileTab}`} style={{ '--diy-code-width': `${split}%` } as CSSProperties}>
      <section className="diy-code-pane" aria-label={t('HTML代码编辑区')}><div className="diy-code-toolbar"><strong><Code2 size={17}/>{t('HTML代码')}</strong><div><button type="button" className="text-btn" disabled={busy} onClick={() => { setPreviewTick(value => value + 1); setStatus(t('已请求检查当前代码。')); }}>{t('检查代码')}</button><button type="button" className="text-btn" disabled={busy || archived} onClick={() => { if (html && !window.confirm(t('确定用示例替换当前代码吗？'))) return; setHTML(exampleHTML); }}>{t('使用示例')}</button></div></div><div className="diy-code-content"><div className="diy-line-numbers" aria-hidden="true"><div style={{ transform: `translateY(-${scrollTop}px)` }}>{Array.from({ length: Math.min(lineCount, 10000) }, (_, index) => <span key={index}>{index + 1}</span>)}</div></div><textarea ref={source} aria-label={t('页面HTML代码')} value={html} disabled={busy || archived} spellCheck={false} wrap="off" placeholder={exampleHTML} onChange={event => { setHTML(event.target.value); setStatus(''); }} onKeyDown={editKey} onScroll={event => setScrollTop(event.currentTarget.scrollTop)}/></div><div className="diy-code-footer"><span>{bytes(documentBytes)} / 5 MiB</span><span>{t('代码仅在隔离环境中运行')}</span></div></section>
      <div className="diy-divider" role="separator" aria-label={t('调整代码与预览宽度')} aria-orientation="vertical" aria-valuenow={split} aria-valuemin={30} aria-valuemax={70} tabIndex={0} onPointerDown={moveDivider} onDoubleClick={() => setSplit(50)} onKeyDown={event => { if (event.key === 'ArrowLeft') { event.preventDefault(); setSplit(value => Math.max(30, value - 5)); } if (event.key === 'ArrowRight') { event.preventDefault(); setSplit(value => Math.min(70, value + 5)); } if (event.key === 'Home') { event.preventDefault(); setSplit(50); } }}/>
      <div className="diy-preview-pane"><div className="diy-context-toolbar"><span>{t('模拟应用')}</span><div className="diy-segment"><button type="button" aria-pressed={mode === 'apk'} onClick={() => setMode('apk')}>APK</button><button type="button" aria-pressed={mode === 'pwa'} onClick={() => setMode('pwa')}>PWA</button></div></div><DIYLandingPreview html={html} config={config} mode={mode} onIssues={setIssues} refreshToken={previewTick}/></div>
    </div>
    <section className="diy-diagnostics"><div className="diy-diagnostics-heading"><div><Settings2 size={16}/><strong>{t('按钮与页面设置')}</strong><span>{config.platform_button ? t('平台主按钮已开启') : config.primary_selector ? t('已配置主按钮选择器') : t('使用代码中的按钮标记')}</span></div><button type="button" className="text-btn" disabled={busy || archived} onClick={() => setSettings(true)}>{t('设置按钮')}</button></div><div className="diy-diagnostics-body"><p>{t('主按钮统一使用应用的下载、安装或外部跳转流程，回流与计费由平台处理。')}</p>{issues.length > 0 && <ul>{issues.map((issue, index) => <li key={`${issue.code || ''}-${index}`} className={issue.severity === 'error' ? 'error' : ''}><button type="button" disabled={!issue.line} onClick={() => focusIssue(issue)}>{issue.line ? `${t('第 {0} 行', { 0: issue.line })}：` : ''}{t(issue.message)}</button></li>)}</ul>}</div></section>

    {settings && <Modal title={t('按钮与页面设置')} close={() => setSettings(false)}><Field label={t('适用应用')}><span className="diy-target-options">{(['apk', 'pwa'] as const).map(kind => <label key={kind}><input type="checkbox" checked={config.targets.includes(kind)} onChange={event => setConfig(current => ({ ...current, targets: event.target.checked ? [...current.targets, kind] : current.targets.filter(value => value !== kind) }))}/>{kind.toUpperCase()}</label>)}</span></Field><Field label={t('主按钮选择器（选填）')} hint={t('填写已有按钮的CSS选择器，例如 #install 或 .download-button。也可在代码中添加 data-d1-action="primary"。')}><input value={config.primary_selector} maxLength={300} onChange={event => setConfig(current => ({ ...current, primary_selector: event.target.value }))} placeholder="#install"/></Field><label className="switch-field"><input type="checkbox" checked={config.platform_button} onChange={event => setConfig(current => ({ ...current, platform_button: event.target.checked }))}/><span>{t('显示平台主按钮')}<small>{t('页面没有可用安装按钮时，访客仍可通过平台按钮继续。')}</small></span></label><Field label={t('资源基址（选填）')} hint={t('用于解析代码中的相对图片和样式路径，必须是HTTPS地址。')}><input type="url" value={config.resource_base} onChange={event => setConfig(current => ({ ...current, resource_base: event.target.value }))} placeholder="https://example.com/assets/"/></Field><p className="diy-modal-note">{t('应用的外部跳转、回流页与安装设置在APK或PWA中配置，同一页面可复用。')}</p><div className="modal-footer"><button type="button" className="btn primary" onClick={() => setSettings(false)}>{t('完成')}</button></div></Modal>}
    {showPages && <Modal side title={t('我的落地页')} close={() => setShowPages(false)}><form className="diy-list-search" onSubmit={event => { event.preventDefault(); setQuery(search); setPage(1); }}><Search size={17}/><input aria-label={t('搜索落地页')} value={search} onChange={event => setSearch(event.target.value)} placeholder={t('按名称搜索')}/><button type="submit" className="btn secondary">{t('搜索')}</button></form><FormError>{listError}</FormError>{listBusy ? <Spinner/> : !listing.data.length ? <Empty title={t('还没有落地页')} detail={t('粘贴代码并保存，页面就会出现在这里。')}/> : <div className="diy-page-list">{listing.data.map(entry => <article key={entry.id} className="diy-page-row"><div className="diy-page-title"><FileCode2 size={21}/><div><button type="button" className="diy-page-open" onClick={() => openPage(entry)}>{entry.name}</button><p>{date(entry.updated_at, true)}</p></div><Badge tone={entry.archived_at ? 'gray' : entry.latest_published_revision_id ? 'green' : 'amber'}>{entry.archived_at ? t('已归档') : entry.latest_published_revision_id ? `v${entry.latest_revision_no || 1}` : t('草稿')}</Badge></div><div className="diy-page-meta"><span>{(entry.draft_config?.targets || []).map(kind => kind.toUpperCase()).join(' / ')}</span><button type="button" className="text-btn" onClick={() => void viewBindings(entry)}><Users size={13}/>{t('{0} 个应用引用', { 0: entry.binding_count || 0 })}</button></div><div className="row-actions wrap"><button type="button" className="text-btn" onClick={() => openPage(entry)}>{t('编辑')}</button><button type="button" className="text-btn" disabled={busy} onClick={() => void copyPage(entry)}><Copy size={13}/>{t('复制')}</button><button type="button" className="text-btn" disabled={busy} onClick={() => void archivePage(entry)}><Archive size={13}/>{entry.archived_at ? t('恢复') : t('归档')}</button><button type="button" className="text-btn danger-text" disabled={busy} onClick={() => setDeletePage(entry)}><Trash2 size={13}/>{t('删除')}</button></div></article>)}</div>}<Pager page={page} total={listing.total} setPage={setPage}/></Modal>}
    {showVersions && item && <Modal title={t('历史版本')} close={() => setShowVersions(false)}><p className="diy-modal-note">{t('已绑定应用继续使用原版本。载入历史代码后，可以另存为新的发布版本。')}</p><FormError>{error}</FormError>{!item.revisions?.length ? <Empty title={t('还没有发布版本')}/> : <div className="diy-version-list">{item.revisions.map(revision => <article key={revision.id}><div><strong>v{revision.revision_no}</strong><small>{date(revision.published_at, true)}</small></div><div className="row-actions"><button type="button" className="text-btn" disabled={busy || archived} onClick={() => void loadRevision(revision)}>{t('载入代码')}</button>{revision.id !== item.latest_published_revision_id && <button type="button" className="text-btn danger-text" disabled={busy} onClick={() => void deleteRevision(revision)}>{t('删除')}</button>}</div></article>)}</div>}</Modal>}
    {bindings && <Modal title={t('引用此页面的应用')} close={() => setBindings(undefined)}><h3>{bindings.page.name}</h3><FormError>{bindings.error}</FormError>{!bindings.data && !bindings.error ? <Spinner/> : !bindings.data?.length ? <Empty title={t('暂无应用引用')}/> : <div className="diy-version-list">{bindings.data.map(binding => <article key={`${binding.resource_type}-${binding.resource_id}`}><div><strong>{binding.resource_name || binding.name || binding.resource_id}</strong><small>{binding.resource_type.toUpperCase()}{binding.revision_no ? ` · v${binding.revision_no}` : ''}</small></div><Link className="text-btn" target="_blank" rel="noopener noreferrer" to={binding.resource_type === 'apk' ? `/apps/${binding.resource_id}` : `/pwas/${binding.resource_id}/edit`}>{t('编辑应用')}</Link></article>)}</div>}</Modal>}
    {deletePage && <Modal title={t('删除落地页')} close={() => setDeletePage(undefined)}><p>{t('确定删除落地页“{0}”及其历史版本吗？已被应用引用的页面不能删除。', { 0: deletePage.name })}</p><div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={() => setDeletePage(undefined)}>{t('取消')}</button><button type="button" className="btn danger" disabled={busy} onClick={() => void removePage()}>{t('确认删除')}</button></div></Modal>}
  </div>;
}
