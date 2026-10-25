(function () {
 'use strict';
 if (window.D1ExternalBrowser) return;
 let config = {};
 try { config = JSON.parse(document.body.dataset.externalBrowser || '{}'); } catch (_) {}
 const zh = /^zh/i.test(document.documentElement.lang || '');
 const text = (en, cn) => zh ? cn : en;
 const selector = '#install-button,#app .install-button,#app .stay-prompt-install,.d1-download-form button,#d1-native-download button';
 let timer, lastRequest = 0, previousFocus;
 const inApp = () => /tiktok|musical_ly|trill|bytedancewebview|aweme|fban|fbav|fb_iab|instagram|micromessenger|\bline\/|;\s*wv\b|\bwebview\b/i.test(navigator.userAgent || '');
 function destination() {
  if (!config?.enabled || !config.click_token || config.continue || document.body.classList.contains('d1-apk-preview')) return null;
  try {
   const target = new URL(config.url);
   const relay = new URL(config.open_url);
   if (!/^https?:$/.test(target.protocol) || target.username || target.password ||
       relay.origin !== window.location.origin || !/^\/(a|p)\/[^/]+\/external\/open$/.test(relay.pathname) || relay.hash || relay.search) return null;
   // On the returned install page, an ordinary browser resumes installation.
   if (config.self_target && !inApp()) return null;
   return { target, relay };
  } catch (_) { return null; }
 }
 function closeGuide() {
  document.getElementById('d1-external-guide')?.remove();
  if (previousFocus?.isConnected) previousFocus.focus();
 }
 function guide() {
  if (!destination() || document.getElementById('d1-external-guide')) return;
  const overlay = document.createElement('div'); overlay.id = 'd1-external-guide';
  const dialog = document.createElement('section'); dialog.setAttribute('role','dialog'); dialog.setAttribute('aria-modal','true');
  const heading = document.createElement('h2'); heading.id = 'd1-external-title'; heading.textContent = text('Open in your browser','在外部浏览器中打开');
  dialog.setAttribute('aria-labelledby',heading.id);
  const description = document.createElement('p'); description.textContent = text('Not redirected? Use the app menu to open in your browser, or copy this link. Your current app may block external opening.','未跳转？请使用应用菜单选择在浏览器中打开，或复制下方链接。当前应用可能限制外部打开。');
  const input = document.createElement('input'); input.readOnly = true; input.value = config.url; input.setAttribute('aria-label',text('Destination URL','目标网址'));
  const tools = document.createElement('div'); tools.className = 'd1-external-tools';
  const copy = document.createElement('button'); copy.type = 'button'; copy.textContent = text('Copy link','复制链接');
  copy.addEventListener('click',async () => {
   input.select();
   try { if (!navigator.clipboard?.writeText) throw new Error('unavailable'); await navigator.clipboard.writeText(config.url); copy.textContent=text('Copied','已复制'); }
   catch (_) { copy.textContent=text('Select and copy the link','请选中链接手动复制'); }
  });
  const direct = document.createElement('a'); direct.href = config.url; direct.textContent = text('Visit directly','直接访问'); direct.target = '_self'; direct.rel = 'noreferrer';
  const note = document.createElement('small'); note.textContent = text('Direct visits may remain in this app.','直接访问可能仍在当前应用内打开。');
  const close = document.createElement('button'); close.type = 'button'; close.className = 'd1-external-close'; close.textContent = text('Close','关闭'); close.addEventListener('click',closeGuide);
  tools.append(copy,direct); dialog.append(heading,description,input,tools,note,close); overlay.append(dialog);
  overlay.addEventListener('click',event => { if (event.target === overlay) closeGuide(); });
  overlay.addEventListener('keydown',event => {
   if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); closeGuide(); }
   if (event.key === 'Tab') {
    const nodes = [input,copy,direct,close], first=nodes[0], last=nodes[nodes.length-1];
    if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
    else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
   }
  });
  previousFocus=document.activeElement; document.body.append(overlay); close.focus();
 }
 function open() {
  const value = destination(); if (!value) return false;
  if (Date.now()-lastRequest < 600) return true;
  lastRequest=Date.now(); clearTimeout(timer);
  const ua = navigator.userAgent || '';
  const android = /android/i.test(ua);
  const ios = /iphone|ipad|ipod/i.test(ua) || (/Macintosh/i.test(ua) && navigator.maxTouchPoints > 1);
  const bytes = new Uint8Array(16);
  if (window.crypto?.getRandomValues) window.crypto.getRandomValues(bytes);
  else for (let i=0;i<bytes.length;i++) bytes[i]=Math.floor(Math.random()*256);
  const key=Array.from(bytes,v=>v.toString(16).padStart(2,'0')).join('');
  value.relay.searchParams.set('key',key);value.relay.searchParams.set('token',config.click_token);
  // Start the report synchronously; never await before a browser handoff.
  // The relay uses the same key if unloading cancels this request.
  try { fetch(new URL('click',config.open_url).href,{method:'POST',credentials:'same-origin',keepalive:true,cache:'no-store',headers:{'Content-Type':'application/json'},body:JSON.stringify({key,token:config.click_token})}).catch(()=>{}); } catch (_) {}
  let href = value.relay.href;
  if (inApp() && android) {
   const fallback = new URL(window.location.href); fallback.searchParams.set('__d1_browser_fallback','1');
   href = 'intent://' + value.relay.host + value.relay.pathname + value.relay.search +
    '#Intent;scheme=' + value.relay.protocol.slice(0,-1) +
    ';S.browser_fallback_url=' + encodeURIComponent(fallback.href) + ';end';
  } else if (inApp() && ios) {
   href = value.relay.href.replace(/^https:/,'x-safari-https:').replace(/^http:/,'x-safari-http:');
  }
  const status=document.getElementById('download-status') || document.getElementById('install-status');
  if (status) status.textContent=text('External opening requested.','已发起外部打开请求。');
  // No timer navigation, await, window.open or fabricated success event.
  try { window.location.href=href; } catch (_) { guide(); return true; }
  if (inApp() && (android || ios)) timer=setTimeout(() => { if (!document.hidden) guide(); },1600);
  return true;
 }
 window.D1ExternalBrowser = { open, returnToken:config?.continue ? config.return_token : undefined };
 document.addEventListener('click',event => {
  const button=event.target?.closest?.(selector);
  if (!button || button.disabled) return;
  if (open()) { event.preventDefault(); event.stopImmediatePropagation(); }
 },true);
 document.addEventListener('submit',event => {
  if (!event.target?.matches?.('.d1-download-form,#d1-native-download')) return;
  if (open()) { event.preventDefault(); event.stopImmediatePropagation(); }
 },true);
 document.addEventListener('visibilitychange',() => { if (document.hidden) clearTimeout(timer); });
 window.addEventListener('pagehide',() => clearTimeout(timer));
 if (config.enabled && new URL(window.location.href).searchParams.get('__d1_browser_fallback') === '1') guide();
})();
