import { useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { send } from './api';
import type { App } from './types';
import { Field, FormError, Modal, errorText } from './ui';

export function AppDownloadSettings({ app, close, onSaved }: { app: App; close: () => void; onSaved?: (app: App) => void }) {
 const [mode,setMode]=useState<'direct'|'info'>(app.download_mode || 'direct');
 const [busy,setBusy]=useState(false);const [error,setError]=useState('');const cache=useQueryClient();
 const save=async(e:FormEvent)=>{e.preventDefault();setBusy(true);setError('');try{const updated=await send<App>(`/apps/${app.id}/download-settings`,{download_mode:mode},'PATCH');onSaved?.(updated);await cache.invalidateQueries();close();}catch(e){setError(errorText(e));}finally{setBusy(false);}};
 return <Modal title="应用设置" close={()=>{if(!busy)close();}}><form onSubmit={save}><p className="modal-intro">{app.name} · 设置分享链接打开后的跳转方式。</p><Field label="跳转方式" hint={mode==='direct'?'访问分享链接后直接开始下载安装包。':'访问分享链接后先展示详情页，用户点击下载按钮后下载安装包。'}><select value={mode} disabled={busy} onChange={e=>setMode(e.target.value as 'direct'|'info')}><option value="direct">直跳下载</option><option value="info">跳转详情页</option></select></Field><p className="small muted">分享链接保持不变，保存后立即生效。</p><FormError>{error}</FormError><div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={close}>取消</button><button className="btn primary" disabled={busy}>{busy?'保存中…':'保存设置'}</button></div></form></Modal>;
}
