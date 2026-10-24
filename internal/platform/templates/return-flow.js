(function () {
 'use strict';
 let raw;
 try { raw = JSON.parse(document.body.dataset.returnFlow || '{}'); } catch (_) { return; }
 if (!raw || raw.enabled !== true) return;
 const zh = (document.documentElement.lang || '').toLowerCase().startsWith('zh');
 const text = (en, cn) => zh ? cn : en;
 const config = {
  ...raw,
  triggerMode: ['back','resume','both'].includes(raw.triggerMode) ? raw.triggerMode : 'back',
  showOnce: raw.showOnce !== false,
  mediaAreaPercent: Math.min(100,Math.max(50,Math.round(Number(raw.mediaAreaPercent) || 50))),
  badgeText: raw.badgeText || text('Limited offer','限时福利'),
  title: raw.title || text('Your exclusive benefit is ready','专属福利已准备好'),
  description: raw.description || text('Come back to claim it. Tap the button to continue.','回来就能领取，点击按钮马上继续。'),
  buttonText: raw.buttonText || text('Claim now','立即领取'),
  dismissText: raw.dismissText || text('Maybe later','稍后再看'),
  buttonAction: ['continue','download','url'].includes(raw.buttonAction) ? raw.buttonAction : 'continue'
 };
 const backEnabled = config.triggerMode === 'back' || config.triggerMode === 'both';
 const resumeEnabled = config.triggerMode === 'resume' || config.triggerMode === 'both';
 const key = 'd1-return-flow:' + window.location.host + window.location.pathname;
 const read = name => { try { return sessionStorage.getItem(name); } catch (_) { return null; } };
 const write = (name,value) => { try { sessionStorage.setItem(name,value); } catch (_) {} };
 const remove = name => { try { sessionStorage.removeItem(name); } catch (_) {} };
 let shown = read(key) === '1';
 let overlay, actionRunning = false, backGuard = false, guardEntries = 0, discardingGuard = false, pendingRearm = false, hiddenAt = 0, handledHiddenAt = 0;
 let riskActive = false, riskInterrupted = false, previousFocus, inertElements = [];
 const hasShown = () => config.showOnce && shown;
 const safeURL = value => {
  if (typeof value !== 'string' || !value.trim() || /[\\\x00-\x20]/.test(value.trim())) return '';
  try {
   const url = new URL(value,window.location.origin);
   const local = url.origin === window.location.origin && url.protocol === 'http:' && ['localhost','127.0.0.1','[::1]'].includes(url.hostname);
   return (url.protocol === 'https:' || local) && !url.username && !url.password ? url.href : '';
  } catch (_) { return ''; }
 };
 const emit = (type,reason) => window.dispatchEvent(new CustomEvent('pwa-return-flow-' + type,{detail:{reason,action:config.buttonAction}}));
 function armBackGuard(force = false) {
  if (!backEnabled || (backGuard && !force) || hasShown()) return;
  if (discardingGuard) { pendingRearm = true; return; }
  try { window.history.pushState({...window.history.state,d1ReturnFlowGuard:true},'',window.location.href); backGuard = true; guardEntries++; } catch (_) {}
 }
 function close(rearm = true) {
  if (overlay) overlay.remove(); overlay = null; actionRunning = false;
  document.documentElement.classList.remove('d1-return-flow-lock');
  for (const [node,previous] of inertElements) node.inert = previous; inertElements = [];
  if (previousFocus && previousFocus.isConnected) previousFocus.focus();
  if (rearm && !config.showOnce) armBackGuard();
 }
 function perform(reason) {
  if (!overlay) return;
  emit('click',reason);
  const target = safeURL(config.buttonUrl);
  if (config.buttonAction === 'download') {
   close(false); actionRunning = true;
   try {
    const result = typeof window.D1ReturnFlowInstall === 'function' ? window.D1ReturnFlowInstall() : undefined;
    Promise.resolve(result).catch(() => {}).finally(() => { actionRunning = false; if (!config.showOnce) armBackGuard(); });
   } catch (_) { actionRunning = false; if (!config.showOnce) armBackGuard(); }
   return;
  }
  // Match the reference's legacy configuration: a URL also overrides Continue.
  if (target) { close(false); actionRunning = true; window.location.assign(target); return; }
  close();
 }
 function show(reason) {
  if (actionRunning || hasShown() || document.hidden) return false;
  shown = true; if (config.showOnce) write(key,'1'); actionRunning = true;
  previousFocus = document.activeElement;
  overlay = document.createElement('div'); overlay.id = 'd1-return-flow-overlay'; overlay.className = 'd1-return-flow-overlay';
  overlay.setAttribute('role','dialog'); overlay.setAttribute('aria-modal','true'); overlay.setAttribute('aria-labelledby','d1-return-flow-title');
  const shell = document.createElement('div'); shell.className = 'd1-return-flow-shell';
  const card = document.createElement('section'); card.className = 'd1-return-flow-card d1-return-flow-area-' + config.mediaAreaPercent;
  const mediaURL = safeURL(config.mediaUrl);
  if (mediaURL) {
   if (config.mediaAreaPercent === 100) card.classList.add('d1-return-flow-cover');
   const media = document.createElement('div'); media.className = 'd1-return-flow-media';
   const video = /\.(mp4|webm|mov|m4v)(?:$|[?#])/i.test(mediaURL);
   const item = document.createElement(video ? 'video' : 'img'); item.src = mediaURL;
   if (video) { item.muted = true; item.loop = true; item.autoplay = true; item.playsInline = true; item.preload = 'metadata'; item.setAttribute('playsinline',''); item.setAttribute('webkit-playsinline',''); }
   else item.alt = '';
   item.addEventListener('error',() => { media.remove(); card.classList.remove('d1-return-flow-cover'); });
   media.appendChild(item); card.appendChild(media);
  }
  const content = document.createElement('div'); content.className = 'd1-return-flow-content';
  const badge = document.createElement('span'); badge.className = 'd1-return-flow-badge'; badge.textContent = config.badgeText;
  const title = document.createElement('h2'); title.id = 'd1-return-flow-title'; title.textContent = config.title;
  const description = document.createElement('p'); description.textContent = config.description;
  const dismiss = document.createElement('button'); dismiss.type = 'button'; dismiss.className = 'd1-return-flow-dismiss'; dismiss.textContent = config.dismissText;
  const action = document.createElement('button'); action.type = 'button'; action.className = 'd1-return-flow-action'; action.textContent = config.buttonText;
  content.append(badge,title,description,dismiss); card.appendChild(content); shell.append(card,action); overlay.appendChild(shell);
  for (const node of Array.from(document.body.children)) { inertElements.push([node,node.inert]); node.inert = true; }
  document.body.appendChild(overlay); document.documentElement.classList.add('d1-return-flow-lock');
  dismiss.addEventListener('click',() => close()); action.addEventListener('click',() => perform(reason));
  overlay.addEventListener('keydown',event => {
   if (event.key === 'Escape') { event.preventDefault(); close(); }
   if (event.key === 'Tab') { event.preventDefault(); (document.activeElement === action ? dismiss : action).focus(); }
  });
  action.focus(); emit('view',reason); return true;
 }
 function markHidden() {
  if (riskActive) { riskInterrupted = true; return; }
  if (resumeEnabled) { hiddenAt = Math.max(Date.now(),hiddenAt + 1); write(key + ':hidden',String(hiddenAt)); }
 }
 function resume() {
  if (document.hidden) return;
  if (riskActive) {
   if (!riskInterrupted) return;
   riskActive = false; riskInterrupted = false; remove(key + ':hidden'); hiddenAt = 0;
   show('download_risk_cancel'); return;
  }
  if (!resumeEnabled || actionRunning) return;
  const timestamp = Number(read(key + ':hidden') || hiddenAt || 0);
  if (!timestamp || timestamp === handledHiddenAt) return;
  handledHiddenAt = timestamp; hiddenAt = 0; remove(key + ':hidden'); show('resume');
 }
 if (backEnabled && !hasShown()) {
  armBackGuard();
  const interactionEvents = ['pointerdown','touchstart','mousedown','keydown','scroll'];
  const onInteraction = () => {
   for (const name of interactionEvents) window.removeEventListener(name,onInteraction,true);
   // Match the reference: arm again after a real gesture so browser back
   // interventions do not skip an entry created before user interaction.
   armBackGuard(true);
  };
  for (const name of interactionEvents) window.addEventListener(name,onInteraction,true);
  window.addEventListener('popstate',() => {
   if (discardingGuard) { discardingGuard = false; if (pendingRearm) { pendingRearm = false; armBackGuard(); } return; }
   if (!backGuard) return;
   backGuard = false; guardEntries = Math.max(0,guardEntries - 1);
   const visible = show('back');
   const remaining = guardEntries; guardEntries = 0;
   // Remove any pre-gesture guard as well, so closing a one-shot popup does
   // not leave an extra history entry or trap the next Back navigation.
   const steps = visible ? remaining : remaining + 1;
   if (!visible) close(false);
   if (steps) { try { discardingGuard = true; window.history.go(-steps); } catch (_) { discardingGuard = false; } }
  });
 }
 document.addEventListener('visibilitychange',() => { if (document.hidden) markHidden(); else resume(); });
 window.addEventListener('pagehide',markHidden);
 window.addEventListener('pageshow',resume);
 window.addEventListener('focus',resume);
 window.addEventListener('blur',() => { if (riskActive) riskInterrupted = true; });
 window.addEventListener('d1-apk-download-start',event => {
  if (!config.showOnDownloadRiskCancel || hasShown() || event.detail?.source === 'return_flow') return;
  riskActive = true; riskInterrupted = document.hidden === true; hiddenAt = 0; remove(key + ':hidden');
 });
})();
