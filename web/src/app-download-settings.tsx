// i18n: static UI literals
import { t, useLocale } from './i18n';
import { useId, useRef, useState, type FormEvent, type KeyboardEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { Download, FileText } from 'lucide-react';
import { send } from './api';
import type { App } from './types';
import { FormError, Modal, errorText } from './ui';
import './app-download-settings.css';

const downloadModes = [
 { value: 'direct', get label() { return t("直跳下载"); }, Icon: Download, get description() { return t("访问分享链接后直接开始下载安装包。"); } },
 { value: 'info', get label() { return t("跳转详情页"); }, Icon: FileText, get description() { return t("访问分享链接后先展示详情页，用户点击下载按钮后下载安装包。"); } },
] as const;

export function AppDownloadSettings({ app, close, onSaved }: { app: App; close: () => void; onSaved?: (app: App) => void }) {
  useLocale();
 const [mode,setMode]=useState<'direct'|'info'>(app.download_mode || 'direct');
 const [busy,setBusy]=useState(false);const [error,setError]=useState('');const cache=useQueryClient();
 const id=useId();
 const tabs=useRef<(HTMLButtonElement|null)[]>([]);
 const navigateTab=(event:KeyboardEvent<HTMLButtonElement>,index:number)=>{
  if(busy)return;
  let next=index;
  if(event.key==='ArrowRight')next=(index+1)%downloadModes.length;
  else if(event.key==='ArrowLeft')next=(index+downloadModes.length-1)%downloadModes.length;
  else if(event.key==='Home')next=0;
  else if(event.key==='End')next=downloadModes.length-1;
  else return;
  event.preventDefault();setMode(downloadModes[next].value);tabs.current[next]?.focus();
 };
 const save=async(e:FormEvent)=>{e.preventDefault();setBusy(true);setError('');try{const updated=await send<App>(`/apps/${app.id}/download-settings`,{download_mode:mode},'PATCH');onSaved?.(updated);await cache.invalidateQueries();close();}catch(e){setError(errorText(e));}finally{setBusy(false);}};
 return <Modal title={t("应用设置")} close={()=>{if(!busy)close();}}><form onSubmit={save}>
  <p className="modal-intro">{app.name} {t(" · 设置分享链接打开后的跳转方式。")}</p>
  <div className="download-mode-field">
   <span className="download-mode-label" id={`${id}-label`}>{t("跳转方式")}</span>
   <div className="download-mode-tabs" role="tablist" aria-labelledby={`${id}-label`}>
    {downloadModes.map(({value,label,Icon},index)=><button key={value} ref={element=>{tabs.current[index]=element;}} type="button" role="tab" className="download-mode-tab" id={`${id}-${value}-tab`} aria-selected={mode===value} aria-controls={`${id}-${value}-panel`} tabIndex={mode===value?0:-1} disabled={busy} onClick={()=>setMode(value)} onKeyDown={event=>navigateTab(event,index)}><Icon size={17} aria-hidden="true"/><span>{label}</span></button>)}
   </div>
   {downloadModes.map(({value,description})=><div key={value} role="tabpanel" className="download-mode-description" id={`${id}-${value}-panel`} aria-labelledby={`${id}-${value}-tab`} tabIndex={0} hidden={mode!==value}>{description}</div>)}
  </div>
  <p className="small muted">{t("分享链接保持不变，保存后立即生效。")}</p><FormError>{error}</FormError>
  <div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={close}>{t("取消")}</button><button className="btn primary" disabled={busy}>{busy?t("保存中…"):t("保存设置")}</button></div>
 </form></Modal>;
}
