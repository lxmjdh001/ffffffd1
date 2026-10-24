import { useState } from 'react';
import { createPortal } from 'react-dom';
import { useQuery } from '@tanstack/react-query';
import { Download, Search } from 'lucide-react';
import { api } from './api';
import { t } from './i18n';
import type { App, Page } from './types';
import type { PWAApp } from './pwa-types';
import { Empty, ErrorState, FormError, Modal, Pager, Spinner, errorText } from './ui';
import './app-config-import.css';

export function AppConfigImport({ kind, disabled, apply }: { kind: 'apps' | 'pwas'; disabled?: boolean; apply: (id: string) => Promise<void> }) {
 const [open, setOpen] = useState(false);
 return <><button type="button" className="btn config-import-button" disabled={disabled} onClick={() => setOpen(true)}><Download size={16}/>{t('导入已有应用配置')}</button>{open && createPortal(<ImportPicker kind={kind} close={() => setOpen(false)} apply={apply}/>, document.body)}</>;
}

function ImportPicker({ kind, close, apply }: { kind: 'apps' | 'pwas'; close: () => void; apply: (id: string) => Promise<void> }) {
 const [search, setSearch] = useState(''); const [page, setPage] = useState(1);
 const [busy, setBusy] = useState(''); const [error, setError] = useState('');
 const query = useQuery({ queryKey: ['config-import', kind, page, kind === 'apps' ? search : ''], queryFn: () => api<Page<App | PWAApp>>(kind === 'apps' ? `/apps?page=${page}&per_page=10&q=${encodeURIComponent(search)}` : '/pwas') });
 const matching = (query.data?.data || []).filter(row => kind === 'apps' || row.name.toLowerCase().includes(search.toLowerCase()));
 const rows = kind === 'pwas' ? matching.slice((page - 1) * 10, page * 10) : matching;
 const total = kind === 'pwas' ? matching.length : query.data?.total || 0;
 const choose = async (id: string) => { if (busy) return; setBusy(id); setError(''); try { await apply(id); close(); } catch (failure) { setError(errorText(failure)); } finally { setBusy(''); } };
 return <Modal title={t('导入已有应用配置')} close={() => { if (!busy) close(); }}><p className="modal-intro">{t('选择当前账号的应用，导入后可继续修改；不会修改原应用或复制分享标识、统计数据。')}</p><label className="search-box"><Search size={16}/><input aria-label={t('搜索应用')} placeholder={t('搜索应用')} value={search} disabled={!!busy} onChange={e => { setSearch(e.target.value); setPage(1); }}/></label>{query.error ? <ErrorState error={query.error} retry={() => query.refetch()}/> : query.isPending ? <Spinner/> : !rows.length ? <Empty title={t('暂无可用应用')}/> : <div className="config-import-list">{rows.map(row => <button type="button" key={row.id} disabled={!!busy} onClick={() => choose(row.id)}><span><strong>{row.name}</strong><small>{'target_url' in row ? row.target_url : row.version || row.file_name}</small></span><span>{busy === row.id ? t('加载中…') : t('导入配置')}</span></button>)}</div>}<Pager page={page} total={total} size={10} setPage={next => { if (!busy) setPage(next); }}/><FormError>{error}</FormError></Modal>;
}
