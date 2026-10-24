import { useState, type FormEvent, type ReactNode } from 'react';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { Check, Copy, Globe2 } from 'lucide-react';
import { api, send } from './api';
import { Badge, Empty, ErrorState, Field, FormError, Modal, Spinner, errorText } from './ui';

type ShareDomain = { url: string; remark: string; source: 'platform'|'custom'; audience: string };

export function DomainShareDrawer({ path, close, title = '选择域名复制链接', settingsAction }: { path: string; close: () => void; title?: string; settingsAction?: ReactNode }) {
 const { data,error,refetch }=useQuery({queryKey:['available-download-domains'],queryFn:()=>api<{data:ShareDomain[]}>('/download-domains')});
 const [query,setQuery]=useState('');
 const [messages,setMessages]=useState<Record<string,string>>({});
 const [copied,setCopied]=useState('');
 const [editing,setEditing]=useState<ShareDomain>();
 const [noteDraft,setNoteDraft]=useState('');
 const [noteError,setNoteError]=useState('');
 const [busy,setBusy]=useState(false);
 const cache=useQueryClient();
 const openNote=(d:ShareDomain)=>{setEditing(d);setNoteDraft(d.remark);setNoteError('');};
 const saveNote=async(e:FormEvent)=>{
  e.preventDefault();if(!editing)return;
  setBusy(true);setNoteError('');
  try{
   const result=await send<{remark:string}>('/download-domains/note',{url:editing.url,remark:noteDraft},'PATCH');
   cache.setQueryData<{data:ShareDomain[]}>(['available-download-domains'],previous=>previous?{data:previous.data.map(d=>d.url===editing.url?{...d,remark:result.remark}:d)}:previous);
   await cache.invalidateQueries({queryKey:['available-download-domains']});
   await cache.invalidateQueries({queryKey:['domains']});
   setEditing(undefined);
  }catch(e){setNoteError(errorText(e));}finally{setBusy(false);}
 };
 const copy=async(d:ShareDomain)=>{try{await navigator.clipboard.writeText(d.url+path);setCopied(d.url);setMessages(p=>({...p,[d.url]:''}));}catch{setMessages(p=>({...p,[d.url]:'复制失败，请手动选择链接复制'}));}};
 const visible=data?.data.filter(d=>(d.url+' '+d.remark).toLowerCase().includes(query.toLowerCase()))||[];
 return <>
  <Modal title={title} close={close} side>
   <div className="share-domain-toolbar"><input className="share-domain-search" aria-label="搜索域名或备注" placeholder="搜索域名或备注" value={query} onChange={e=>setQuery(e.target.value)}/>{settingsAction}</div>
   {error?<ErrorState error={error} retry={()=>refetch()}/>:!data?<Spinner/>:!visible.length?<Empty title={query?'没有匹配的域名':'暂无可用域名'} detail="请联系管理员分配域名，或在域名管理中完成 CNAME 验证。"/>:
    <div className="share-domain-list">{visible.map(d=>
     <section className="share-domain-card" key={d.url}>
      <div className="share-domain-head"><Globe2 size={18}/><strong>{d.url}</strong><Badge tone={d.source==='custom'?'violet':'blue'}>{d.source==='custom'?'自定义':d.audience==='paid'?'付费用户':'公共域名'}</Badge></div>
      <div className="share-domain-link">
       {d.remark&&<small className="share-domain-remark" title={d.remark}>{d.remark}</small>}
       <div className="share-domain-copy"><code>{d.url}{path}</code><div className="share-domain-actions">
        <button type="button" className="text-btn" aria-label={`备注 ${d.url}`} onClick={()=>openNote(d)}>备注</button>
        <button type="button" className="btn secondary" onClick={()=>copy(d)}>{copied===d.url?<Check size={14}/>:<Copy size={14}/>} {copied===d.url?'已复制':'复制'}</button>
       </div></div>
      </div>
      {messages[d.url]&&<p className="small" role="status">{messages[d.url]}</p>}
     </section>
    )}</div>}
   <p className="muted small">这里的备注仅用于当前账号区分域名，不会改变下载地址或访问权限。</p>
  </Modal>
  {editing&&<Modal title="编辑域名备注" close={()=>{if(!busy)setEditing(undefined);}}>
   <form onSubmit={saveNote}><p className="modal-intro">{editing.url}</p>
    <Field label="域名备注" hint="最多 120 字，留空保存可清除备注。"><input autoFocus maxLength={120} value={noteDraft} disabled={busy} placeholder="例如：主域名 / 投放 A" onChange={e=>setNoteDraft(e.target.value)}/></Field>
    <FormError>{noteError}</FormError>
    <div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={()=>setEditing(undefined)}>取消</button><button className="btn primary" disabled={busy}>{busy?'保存中…':'保存备注'}</button></div>
   </form>
  </Modal>}
 </>;
}
