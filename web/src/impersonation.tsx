import {reloadAccount} from './account-context';
import {useRef,useState} from 'react';
import {useQueryClient} from '@tanstack/react-query';
import {ArrowLeft,ShieldCheck} from 'lucide-react';
import {send} from './api';
import {t,useLocale} from './i18n';
import {errorText,FormError} from './ui';
import type {ImpersonationInfo} from './types';
import './impersonation.css';

export function ImpersonationBanner({scope}:{scope:ImpersonationInfo}){
 useLocale();const cache=useQueryClient(),submitting=useRef(false),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const restore=async()=>{
  if(submitting.current)return;submitting.current=true;setBusy(true);setError('');
  try{await send('/auth/impersonation/stop',{});await cache.cancelQueries();cache.clear();reloadAccount('/admin/users');}
  catch(failure){setError(errorText(failure));}finally{submitting.current=false;setBusy(false);}
 };
 return <section className="impersonation-banner" aria-label={t('管理员代登录')}><div className="impersonation-banner-row"><ShieldCheck size={20} aria-hidden="true"/><div className="impersonation-banner-copy"><strong>{t('正在以 {0} 的身份操作',{0:scope.member_name})}</strong><span>{scope.available?t('管理员代登录'):t('代登录已到期或会员不可用，请返回管理员')}</span></div><button type="button" className="btn secondary" onClick={()=>void restore()} disabled={busy}><ArrowLeft size={15} aria-hidden="true"/>{busy?t('正在返回…'):t('返回管理员')}</button></div><FormError>{error}</FormError></section>;
}
