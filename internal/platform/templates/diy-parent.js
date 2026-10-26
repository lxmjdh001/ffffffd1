(function(){
 'use strict';
 const frame=document.getElementById('d1-diy-content');if(!frame)return;
 const channel=document.body.dataset.channel,bar=document.getElementById('d1-diy-actions');
 const button=document.getElementById('install-button')||document.querySelector('#d1-native-download button');
 const cancel=document.getElementById('d1-diy-cancel'),confirm=document.getElementById('d1-diy-confirm');
 const initiallyVisible=!bar.hidden;let last=0,ready=false,interacted=false,lastBack=0;
 function sync(){if(!ready||!frame.contentWindow)return;frame.contentWindow.postMessage({type:'d1-diy-state',channel,label:button?.textContent?.trim(),disabled:!!button?.disabled},'*');}
 // A child message can request a visible trusted CTA, never execute a paid
 // action. The next real click invokes the original download/install runtime.
 window.addEventListener('message',event=>{
  const value=event.data;if(event.source!==frame.contentWindow||value?.type!=='d1-diy'||value.channel!==channel)return;
  if(value.action==='ready'){ready=true;sync();return;}
  if(value.action==='interaction'){if(!interacted){interacted=true;window.dispatchEvent(new window.Event('d1-diy-interaction'));}return;}
  if(value.action==='back'){if(Date.now()-lastBack>300){lastBack=Date.now();window.dispatchEvent(new window.Event('d1-diy-back'));}return;}
  if(value.action!=='primary'||!Number.isSafeInteger(value.request)||value.request<=last)return;
  last=value.request;bar.hidden=false;cancel.hidden=false;confirm.hidden=false;button?.focus();
 });
 cancel?.addEventListener('click',()=>{bar.hidden=!initiallyVisible;cancel.hidden=true;confirm.hidden=true;});
 button?.addEventListener('click',()=>{cancel.hidden=true;confirm.hidden=true;});
 if(typeof MutationObserver==='function')new MutationObserver(sync).observe(bar,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['disabled','aria-label']});
 frame.addEventListener('load',()=>{last=0;ready=true;sync();frame.contentWindow?.postMessage({type:'d1-diy-hello',channel},'*');});
 // The child may have finished loading before this deferred script arrived.
 frame.contentWindow?.postMessage({type:'d1-diy-hello',channel},'*');
 // Match the existing templates' configured W2A automatic-download policy.
 const settings=window.__APP_CONFIG__?.app?.settings;
 if(document.body.dataset.kind==='pwa'&&settings?.packagePriority==='w2a_only'&&settings.w2aAutoDownload&&settings.apkUrl&&!settings.externalBrowser?.enabled&&!window.isBrowserGuideEnabled?.()){
  const ua=navigator.userAgent||'';const ios=/iphone|ipad|ipod/i.test(ua)||(/Macintosh/i.test(ua)&&navigator.maxTouchPoints>1);
  if(/android/i.test(ua)||(ios&&settings.iosCompatible))void window.D1ReturnFlowInstall?.();
 }
})();
