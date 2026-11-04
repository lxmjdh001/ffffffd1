import {useEffect} from 'react';

const accountContextKey='d1.account-context';
export function reloadAccount(path:'/'|'/admin/users',broadcast=true){
 if(broadcast){try{localStorage.setItem(accountContextKey,JSON.stringify({path,nonce:crypto.randomUUID()}));}catch{/* Navigation still clears the current tab when storage is unavailable. */}}
 // A hash-only location.replace does not reload modules or identity observers.
 // Commit the destination synchronously, then reload the whole account context.
 window.history.replaceState(null,'','/#'+path);
 window.location.reload();
}
export function useAccountContextSync(){
 useEffect(()=>{
  const changed=(event:StorageEvent)=>{
   if(event.key!==accountContextKey||!event.newValue)return;
   try{const {path}=JSON.parse(event.newValue);if(path==='/'||path==='/admin/users')reloadAccount(path,false);}catch{/* Ignore malformed notifications. */}
  };
  window.addEventListener('storage',changed);return()=>window.removeEventListener('storage',changed);
 },[]);
}
