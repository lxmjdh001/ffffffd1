(function(){
 'use strict';
 const preview=document.body.classList.contains('d1-apk-preview');
 if(!preview&&!document.body.classList.contains('d1-apk-reference'))return;
 let settings=window.__APP_CONFIG__?.d1APK;
 const form=document.getElementById('d1-native-download');const nativeButton=form?.querySelector('button');if(form)form.hidden=true;
 const extras=document.getElementById('d1-download-extras');const info=document.getElementById('d1-file-info');
 const patch=()=>{
  if(!settings)return;
  if(settings.backgroundColor&&settings.backgroundColor!=='#ffffff'){document.body.style.backgroundColor=settings.backgroundColor;const page=document.querySelector('#app .google-play-mobile');if(page)page.style.backgroundColor=settings.backgroundColor;}
  document.body.classList.toggle('d1-apk-no-rating',!settings.rating);
  for(const [key,cls] of [['showDownloads','d1-apk-no-downloads'],['showDescription','d1-apk-no-description'],['showScreenshots','d1-apk-no-screenshots']])document.body.classList.toggle(cls,settings[key]===false);
  const button=document.querySelector('#app .install-button');if(!button)return;
  if(settings.themeColor&&settings.themeColor!=='#01875f'&&document.querySelector('#app .google-play-mobile'))button.style.backgroundColor=settings.themeColor;
  for(const b of document.querySelectorAll('#app .install-button,#app .stay-prompt-install')){
   const node=Array.from(b.childNodes).find(n=>n.nodeType===Node.TEXT_NODE&&n.textContent.trim());
   if(node&&node.textContent.trim()!==settings.buttonText)node.textContent=settings.buttonText;
   if(nativeButton&&b.disabled!==nativeButton.disabled)b.disabled=nativeButton.disabled;
  }
  for(const score of document.querySelectorAll('#app .app-stats-row>.stat-col:first-child .stat-main,#app .large-rating')){
   const node=Array.from(score.childNodes).find(n=>n.nodeType===Node.TEXT_NODE);if(node&&node.textContent.trim()!==settings.rating)node.textContent=settings.rating||'';
  }
  if(extras&&button.nextElementSibling!==extras)button.after(extras);
  const ratings=document.querySelector('#app .ratings-section');if(info&&ratings&&ratings.previousElementSibling!==info)ratings.before(info);
 };
 if(preview)window.addEventListener('message',event=>{if(event.origin!==location.origin||event.source!==window.parent||event.data?.type!=='UPDATE_APP_INFO')return;settings=event.data.data?.d1APK;patch();});
 const observer=new MutationObserver(patch);observer.observe(document.body,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['disabled']});patch();
})();
