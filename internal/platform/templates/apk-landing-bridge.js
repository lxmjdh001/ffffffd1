(function(){
 'use strict';
 const preview=document.body.classList.contains('d1-apk-preview');
 if(!preview&&!document.body.hasAttribute('data-d1-apk-reference'))return;
 let settings=window.__APP_CONFIG__?.d1APK;
 const form=document.getElementById('d1-native-download');const nativeButton=form?.querySelector('button');if(form)form.hidden=true;
 const extras=document.getElementById('d1-download-extras');const info=document.getElementById('d1-file-info');
 // Keep Vue-owned text nodes alive so language/preview updates remain reactive.
 const textStates=new WeakMap(),generatedText=new WeakSet(),labels=new WeakMap(),wrapperSources=new WeakMap();
 const removeNoise=(node,restore)=>{
  const state=textStates.get(node);if(!state)return;
  for(const added of state.added)if(added.parentNode===node.parentNode)added.remove();
  if(state.wrapper.parentNode)state.wrapper.replaceWith(node);
  if(restore)node.nodeValue=state.original;textStates.delete(node);
 };
 const segmenter=typeof Intl.Segmenter==='function'?new Intl.Segmenter(undefined,{granularity:'grapheme'}):null;
 const syncNames=enabled=>{
  const selector='#app .app-title,#app .app-developer-link,#app .stay-prompt-app-name,#app .stay-prompt-app-developer,#app .scan-result-name';
  for(const root of document.querySelectorAll(selector)){
   for(const wrapper of root.querySelectorAll('.d1-obfuscated-text')){const source=wrapperSources.get(wrapper);if(!source||!wrapper.contains(source))wrapper.remove();}
   if(enabled){
    if(!labels.has(root))labels.set(root,root.getAttribute('aria-label'));
    const copy=root.cloneNode(true);copy.querySelectorAll('.d1-text-noise').forEach(n=>n.remove());root.setAttribute('aria-label',copy.textContent.trim());
   }else if(labels.has(root)){
    const label=labels.get(root);if(label===null)root.removeAttribute('aria-label');else root.setAttribute('aria-label',label);labels.delete(root);
   }
   const walker=document.createTreeWalker(root,4),nodes=[];let node;
   while((node=walker.nextNode()))if(!generatedText.has(node)&&!node.parentElement?.closest('.d1-text-noise'))nodes.push(node);
   for(const source of nodes){
    if(!enabled){removeNoise(source,true);continue;}
    if(textStates.has(source)||!source.nodeValue.trim())continue;
    const original=source.nodeValue;
    // Old browsers without grapheme support keep the full visible name intact.
    const parts=segmenter?Array.from(segmenter.segment(original),v=>v.segment):[original];
    const bytes=window.crypto.getRandomValues(new Uint8Array(parts.length*2));
    // Render the complete visible word as one run to preserve kerning, ligatures
    // and flex layout. The underlying text remains the interleaved value.
    const wrapper=document.createElement('span');wrapper.className='d1-obfuscated-text';wrapper.setAttribute('data-d1-display',original);
    const value=document.createElement('span');value.className='d1-obfuscated-value';value.setAttribute('aria-hidden','true');
    source.replaceWith(wrapper);wrapper.append(value);value.append(source);wrapperSources.set(wrapper,source);
    const added=[],fragment=document.createDocumentFragment();source.nodeValue=parts[0];
    for(let i=0;i<parts.length;i++){
     if(i){const text=document.createTextNode(parts[i]);generatedText.add(text);added.push(text);fragment.append(text);}
     const noise=document.createElement('span');noise.className='d1-text-noise';noise.setAttribute('aria-hidden','true');
     noise.textContent=Array.from(bytes.slice(i*2,i*2+2),b=>'abcdefghijklmnopqrstuvwxyz0123456789'[b%36]).join('');
     added.push(noise);fragment.append(noise);
    }
    source.after(fragment);textStates.set(source,{original,added,wrapper});
   }
  }
 };
 const patch=()=>{
  if(!settings)return;
  syncNames(settings.textObfuscation===true);
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
 const refresh=(records=[])=>{
  observer.disconnect();
  // An external Vue update supplies the new full text, even if it happens to
  // equal the first letter of the old name. Discard only our added siblings.
  for(const record of records)if(record.type==='characterData')removeNoise(record.target,false);
  patch();
  observer.observe(document.body,{subtree:true,childList:true,characterData:true,attributes:true,attributeFilter:['disabled']});
 };
 const observer=new MutationObserver(refresh);
 if(preview)window.addEventListener('message',event=>{if(event.origin!==location.origin||event.source!==window.parent||event.data?.type!=='UPDATE_APP_INFO')return;settings=event.data.data?.d1APK;refresh();});
 refresh();
})();
