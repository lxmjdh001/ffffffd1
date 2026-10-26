// i18n: static UI literals
import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { Bookmark, Code2, ExternalLink, RefreshCw } from 'lucide-react';
import { api } from './api';
import { t, useLocale } from './i18n';
import { date, errorText, Field, FormError } from './ui';
import { DIYLandingPreview } from './diy-landing-preview';
import { DIYLandingContentEditor } from './diy-landing-content-editor';
import type { DIYLandingKind, DIYLandingPage, DIYLandingPageSummary, DIYLandingReference, DIYLandingRevision, DIYLandingOptions } from './diy-landing-types';
import './landing-template-tabs.css';

type Favorite = { id: string; name: string; reference: DIYLandingReference; config?: DIYLandingOptions; version?: number };
export type LandingSourceSelectorProps = {
  kind: DIYLandingKind; value?: DIYLandingReference;
  onChange: (reference: DIYLandingReference | undefined) => void; disabled?: boolean; hidePreview?: boolean; onBusyChange?: (busy: boolean) => void;
};

export function LandingSourceSelector({ kind, value, onChange, disabled = false, hidePreview = false, onBusyChange }: LandingSourceSelectorProps) {
  useLocale();
  const [pages, setPages] = useState<DIYLandingPageSummary[]>([]), [favorites, setFavorites] = useState<Favorite[]>([]), [revisions, setRevisions] = useState<DIYLandingRevision[]>([]);
  const [search, setSearch] = useState(''), [error, setError] = useState(''), [busy, setBusy] = useState(true), [refresh, setRefresh] = useState(0);
  const [loadErrors, setLoadErrors] = useState<{favorites?: string; diy?: string}>({});
  const sourceTabs = useRef<HTMLDivElement>(null);
  const [source, setSource] = useState<'favorites' | 'diy'>('favorites'), [pageName, setPageName] = useState('');
  const initialized = useRef(''), manuallySelected = useRef(false);
  useEffect(() => {
    const controller = new AbortController(); setBusy(true); setError('');
    Promise.allSettled([
      api<{ data: DIYLandingPageSummary[] }>(`/diy-landings?published=1&target=${kind}&per_page=200`, { signal: controller.signal }),
      api<{ data: Favorite[] }>(`/landing-favorites?target=${kind}&per_page=200`, { signal: controller.signal }),
    ]).then(([owned, saved]) => {
      if (controller.signal.aborted) return;
      setLoadErrors({diy: owned.status === 'rejected' ? errorText(owned.reason) : undefined, favorites: saved.status === 'rejected' ? errorText(saved.reason) : undefined});
      setPages(owned.status === 'fulfilled' ? owned.value.data || [] : []);
      setFavorites(saved.status === 'fulfilled' ? (saved.value.data || []).filter(entry => entry.reference?.page_id && entry.reference?.revision_id) : []);
      if (owned.status === 'rejected' && saved.status === 'rejected') setError(errorText(owned.reason));
      setBusy(false);
    });
    return () => controller.abort();
  }, [kind, refresh]);
  useEffect(() => {
    const key = `${kind}:${value?.page_id || ''}`;
    if (busy || manuallySelected.current && initialized.current === key) return;
    if (initialized.current !== key) manuallySelected.current = false;
    if (value?.page_id) setSource(favorites.some(entry => entry.reference.page_id === value.page_id && entry.reference.revision_id === value.revision_id) ? 'favorites' : 'diy');
    else if (initialized.current !== key) setSource('favorites');
    initialized.current = key;
  }, [busy, favorites, kind, value?.page_id]);
  useEffect(() => {
    if (!value?.page_id) { setRevisions([]); setPageName(''); return; }
    const controller = new AbortController(); setError('');
    api<DIYLandingPage>(`/diy-landings/${encodeURIComponent(value.page_id)}`, { signal: controller.signal }).then(page => {
      if (controller.signal.aborted) return;
      setPageName(page.name); setRevisions((page.revisions || []).filter(revision => revision.id === value.revision_id || !revision.config?.targets?.length || revision.config.targets.includes(kind)));
      if (page.archived_at) setError(t('此落地页已归档，当前应用可继续使用，不能新增绑定。'));
    }).catch(failure => { if (!controller.signal.aborted) setError(errorText(failure)); });
    return () => controller.abort();
  }, [value?.page_id, value?.revision_id, kind, refresh]);
  const favoritePages = new Map(favorites.map(entry => [entry.reference.page_id, entry.reference.revision_id]));
  const owned = pages.filter(page => favoritePages.get(page.id) !== page.latest_published_revision_id);
  const visible = (source === 'favorites' ? favorites : owned).filter(page => page.name.toLowerCase().includes(search.toLowerCase()));
  const selected = pages.find(page => page.id === value?.page_id);
  const favorite = favorites.find(entry => entry.reference.page_id === value?.page_id && entry.reference.revision_id === value?.revision_id);
  const newer = source === 'diy' && selected?.latest_published_revision_id && selected.latest_published_revision_id !== value?.revision_id;
  const label = source === 'favorites' ? t('已收藏落地页') : t('已发布落地页');
  const changeSource = (next: 'favorites' | 'diy') => { manuallySelected.current = true; setSource(next); setSearch(''); };
  const chooseReference = (next?: DIYLandingReference) => {
    if (value && next?.page_id === value.page_id && next.revision_id === value.revision_id) return;
    onChange(next);
  };

  return <div className="diy-source-selector">
    <div className="diy-source-heading"><h3>{t('选择DIY落地页')}</h3><div><Link to="/diy-landings" target="_blank" rel="noopener noreferrer" className="text-btn">{t('打开DIY落地页')}<ExternalLink size={13}/></Link><button type="button" className="icon-btn" aria-label={t('刷新落地页列表')} disabled={disabled || busy} onClick={() => setRefresh(current => current + 1)}><RefreshCw size={15} className={busy ? 'spin' : ''}/></button></div></div>
    <div ref={sourceTabs} className="landing-source-tabs" role="tablist" aria-label={t('DIY页面来源')} onKeyDown={event=>{if(!['ArrowLeft','ArrowRight','Home','End'].includes(event.key))return;event.preventDefault();const next=event.key==='Home'?'favorites':event.key==='End'?'diy':source==='favorites'?'diy':'favorites';changeSource(next);sourceTabs.current?.querySelectorAll<HTMLButtonElement>('[role=tab]')[next==='favorites'?0:1]?.focus();}}><button type="button" role="tab" aria-selected={source === 'favorites'} disabled={disabled} onClick={() => changeSource('favorites')}><Bookmark size={15}/>{t('我的收藏')}<span className="landing-source-search-count">{favorites.length}</span></button><button type="button" role="tab" aria-selected={source === 'diy'} disabled={disabled} onClick={() => changeSource('diy')}><Code2 size={15}/>{t('我的DIY')}<span className="landing-source-search-count">{owned.length}</span></button></div>
    <Field label={t('搜索落地页')}><input type="search" value={search} disabled={disabled || busy} onChange={event => setSearch(event.target.value)} placeholder={source === 'favorites' ? t('按名称查找收藏页面') : t('按名称查找已发布页面')}/></Field>
    <Field label={label}><select aria-label={label} data-field="diy_landing" required value={value?.page_id || ''} disabled={disabled || busy} onChange={event => {
      setError('');
      if (source === 'favorites') { const favorite = favorites.find(entry => entry.reference.page_id === event.target.value)?.reference; chooseReference(favorite ? { page_id: favorite.page_id, revision_id: favorite.revision_id } : undefined); }
      else { const page = owned.find(entry => entry.id === event.target.value); chooseReference(page?.latest_published_revision_id ? { page_id: page.id, revision_id: page.latest_published_revision_id } : undefined); }
    }}><option value="">{busy ? t('正在加载…') : source === 'favorites' ? t('请选择已收藏落地页') : t('请选择已发布落地页')}</option>{value && !visible.some(entry => (source === 'favorites' ? (entry as Favorite).reference.page_id : entry.id) === value.page_id) && <option value={value.page_id}>{pageName || t('当前绑定页面')}</option>}{visible.map(entry => <option key={entry.id} value={source === 'favorites' ? (entry as Favorite).reference.page_id : entry.id}>{entry.name} · v{source === 'favorites' ? (entry as Favorite).version || 1 : (entry as DIYLandingPageSummary).latest_revision_no || 1}</option>)}</select></Field>
    {value && source === 'diy' && <Field label={t('使用版本')} hint={t('应用锁定所选版本，发布新版本不会自动替换。')}><select aria-label={t('使用版本')} value={value.revision_id} disabled={disabled || !revisions.length} onChange={event => chooseReference({ page_id: value.page_id, revision_id: event.target.value })}>{!revisions.some(revision => revision.id === value.revision_id) && <option value={value.revision_id}>{t('当前绑定版本')}</option>}{revisions.map(revision => <option key={revision.id} value={revision.id}>v{revision.revision_no} · {date(revision.published_at, true)}</option>)}</select></Field>}
    {value && source === 'favorites' && favorite && <p className="landing-source-favorite-note"><Bookmark size={14}/>{t('使用收藏时的固定版本，模版库更新不会改变当前应用。')}</p>}
    {newer && <p className="diy-source-update">{t('此页面有新发布版本，可在使用版本中选择更新。')}</p>}
    {!busy && visible.length === 0 && !loadErrors[source] && <div className="diy-source-empty"><p>{source === 'favorites' ? t('还没有收藏落地页。到模版库收藏后，即可在这里选择。') : t('还没有可用的已发布页面。先创建并发布落地页，再刷新列表。')}</p><div className="landing-source-empty-actions"><Link to="/landing-templates" target="_blank" rel="noopener noreferrer" className="btn secondary"><Bookmark size={14}/>{t('浏览模版库')}</Link></div></div>}
    <FormError>{error || loadErrors[source]}</FormError>{value && <DIYLandingContentEditor key={`${kind}:${value.page_id}:${value.revision_id}`} kind={kind} reference={value} change={onChange} disabled={disabled} onBusyChange={onBusyChange}/>}{value && !hidePreview && <DIYLandingPreview reference={value} mode={kind} compact/>}
  </div>;
}
