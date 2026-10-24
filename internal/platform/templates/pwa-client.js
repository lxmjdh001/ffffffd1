(function () {
 'use strict';
 const base = document.body.dataset.base;
 const zh = document.body.dataset.locale === 'zh';
 const text = (en, cn) => zh ? cn : en;
 const installButton = document.getElementById('install-button');
 const installStatus = document.getElementById('install-status');
 const notificationButton = document.getElementById('notification-button');
 const unsubscribeButton = document.getElementById('unsubscribe-button');
 const notificationStatus = document.getElementById('notification-status');
 const guide = document.getElementById('install-guide');
 const guideText = document.getElementById('install-guide-text');
 const storeKey = 'd1-pwa-subscription:' + base;
 let installPrompt;
 let registration;
 let config;
 let busy = false;
 let pushReady = false;
 if (notificationButton) notificationButton.disabled = true;
 const setStatus = (node, value) => { if (node) node.textContent = value; };
 const storageRead = () => { try { return JSON.parse(localStorage.getItem(storeKey) || 'null'); } catch (_) { return null; } };
 const storageWrite = (v) => { try { if (v) localStorage.setItem(storeKey, JSON.stringify(v)); else localStorage.removeItem(storeKey); } catch (_) {} };
 const supported = window.isSecureContext && 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
 const isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent) || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
 const standalone = window.matchMedia('(display-mode: standalone)').matches || navigator.standalone === true;
 function showGuide() {
  const message = isIOS
   ? text('Open this page in Safari. Tap Share, then “Add to Home Screen”. Open the installed app to enable notifications (iOS 16.4 or later).', '请用 Safari 打开此页面，点击“分享”，再选择“添加到主屏幕”。从桌面打开应用后可开启通知（需 iOS 16.4 或以上）。')
   : text('Use your browser menu and choose “Install app” or “Add to Home screen”. If neither is available, try a current version of Chrome, Edge, or Safari.', '请在浏览器菜单中选择“安装应用”或“添加到主屏幕”。若没有此选项，请使用较新版本的 Chrome、Edge 或 Safari。');
  if (guide && guideText) { guide.hidden = false; guideText.textContent = message; }
  setStatus(installStatus, message);
 }
 window.addEventListener('beforeinstallprompt', (event) => { event.preventDefault(); installPrompt = event; if (installButton) installButton.disabled = false; });
 window.addEventListener('appinstalled', () => { installPrompt = null; setStatus(installStatus, text('App installed. Open it from your home screen.', '应用已安装，可以从桌面打开。')); });
 if (standalone) setStatus(installStatus, text('You are using the installed app.', '你正在使用桌面应用。'));
 if (installButton) installButton.addEventListener('click', async () => {
  if (!installPrompt) { showGuide(); return; }
  const prompt = installPrompt; installPrompt = null; installButton.disabled = true;
  try { await prompt.prompt(); const choice = await prompt.userChoice; setStatus(installStatus, choice.outcome === 'accepted' ? text('Installation started. Look for the app on your home screen.', '已开始安装，请在桌面查看应用。') : text('Installation canceled. You can install it later.', '已取消安装，你可以随时重新安装。')); }
  catch (_) { showGuide(); } finally { installButton.disabled = false; }
 });
 async function post(url, body) {
  const res = await fetch(url, {method:'POST',credentials:'omit',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
  let result = {}; try { result = await res.json(); } catch (_) {}
  if (!res.ok) throw new Error(result.error || text('Please try again.', '请稍后重试。'));
  return result;
 }
 function keyBytes(key) { const padded = key.replace(/-/g,'+').replace(/_/g,'/') + '='.repeat((4-key.length%4)%4); return Uint8Array.from(atob(padded), ch => ch.charCodeAt(0)); }
 function activeUI(active) {
  if (notificationButton) notificationButton.textContent = active ? text('Notifications on', '通知已开启') : text('Enable notifications', '开启通知');
  if (unsubscribeButton) unsubscribeButton.hidden = !active;
 }
 async function setup() {
  if (document.body.dataset.pushEnabled !== 'true') { if (notificationButton) notificationButton.hidden = true; setStatus(notificationStatus,text('Notifications are currently unavailable for this app.', '此应用暂未开启通知功能。')); }
  if (!('serviceWorker' in navigator) || !window.isSecureContext) { if (notificationButton) notificationButton.disabled = true; setStatus(notificationStatus,text('This browser does not support web app notifications.', '当前浏览器不支持网页应用通知。')); return; }
  try {
   // Explicit scope prevents this worker from controlling the console or other apps.
   registration = await navigator.serviceWorker.register(base+'sw.js', {scope:base,updateViaCache:'none'});
   await navigator.serviceWorker.ready;
   if (!supported) { if (notificationButton) notificationButton.disabled = true; setStatus(notificationStatus,text('Notifications are not supported in this browser. On iPhone or iPad, install the app first and open it from the home screen.', '当前浏览器不支持通知。iPhone 或 iPad 请先添加到主屏幕，再从桌面打开。')); return; }
   const response = await fetch(base+'push-config', {credentials:'omit',cache:'no-store'});
   if (!response.ok) throw new Error('config'); config = await response.json();
   const subscription = await registration.pushManager.getSubscription();
   const saved = storageRead(); activeUI(!!subscription && !!saved);
   if (!config.enabled) { if (notificationButton) notificationButton.hidden = true; return; }
   pushReady = true; if (notificationButton) notificationButton.disabled = false;
   if (Notification.permission === 'denied') { setStatus(notificationStatus,text('Notifications are blocked. Enable them in your browser or device settings.', '通知已被阻止，请在浏览器或设备设置中开启。')); return; }
   if (subscription && saved) setStatus(notificationStatus,text('You are subscribed. You can turn notifications off at any time.', '你已订阅，可以随时关闭通知。'));
   else if (isIOS && !standalone) setStatus(notificationStatus,text('Install the app and open it from your home screen to enable notifications.', '请先添加到主屏幕，再从桌面打开应用以开启通知。'));
  } catch (_) { setStatus(notificationStatus,text('Unable to prepare notifications. Refresh the page to try again.', '通知服务暂不可用，请刷新后重试。')); }
 }
 if (notificationButton) notificationButton.addEventListener('click', async () => {
  if (busy || !pushReady) return;
  if (!supported || (isIOS && !standalone)) { showGuide(); setStatus(notificationStatus,text('Install the app first, then open it from the home screen.', '请先安装应用，再从桌面打开。')); return; }
  if (Notification.permission === 'denied') { setStatus(notificationStatus,text('Notifications are blocked. Enable them in browser or device settings.', '通知已被阻止，请到浏览器或设备设置中开启。')); return; }
  busy = true; notificationButton.disabled = true;
  try {
   // Permission must begin directly in the user's click gesture, before fetching.
   const permission = Notification.permission === 'granted' ? 'granted' : await Notification.requestPermission();
   if (permission !== 'granted') { setStatus(notificationStatus,text('Notifications were not enabled. You can try again later.', '尚未开启通知，你可以稍后重试。')); return; }
   if (!registration || !config || !config.enabled || !config.public_key) throw new Error(text('Notifications are not ready. Refresh and try again.', '通知服务未就绪，请刷新后重试。'));
   let subscription = await registration.pushManager.getSubscription();
   const newlyCreated = !subscription;
   if (!subscription) subscription = await registration.pushManager.subscribe({userVisibleOnly:true,applicationServerKey:keyBytes(config.public_key)});
   const serialized = subscription.toJSON(); const body = {endpoint:serialized.endpoint,keys:serialized.keys}; const saved = storageRead(); if (saved) body.token = saved.token;
   try {
    const result = await post(config.subscribe_url, body);
    if (!result.subscription_id || !result.token) throw new Error(text('Subscription could not be saved.', '订阅保存失败。'));
    storageWrite({subscription_id:result.subscription_id,token:result.token}); activeUI(true);
    setStatus(notificationStatus,text('Notifications enabled. You are all set.', '通知已开启，订阅成功。'));
   } catch (error) { if (newlyCreated) await subscription.unsubscribe().catch(() => {}); throw error; }
  } catch (error) { setStatus(notificationStatus,error.message || text('Unable to enable notifications. Try again later.', '暂时无法开启通知，请稍后重试。')); }
  finally { busy=false; notificationButton.disabled=!pushReady; }
 });
 if (unsubscribeButton) unsubscribeButton.addEventListener('click', async () => {
  if (busy || !registration || !config) return; busy=true; unsubscribeButton.disabled=true;
  try {
   const saved=storageRead(); if (!saved) throw new Error(text('Subscription details are unavailable. Turn off notifications in browser settings.', '订阅信息不可用，请在浏览器设置中关闭通知。'));
   await post(config.unsubscribe_url,saved);
   const subscription=await registration.pushManager.getSubscription(); if (subscription) await subscription.unsubscribe();
   storageWrite(null); activeUI(false); setStatus(notificationStatus,text('Notifications turned off.', '通知已关闭。'));
  } catch (error) { setStatus(notificationStatus,error.message || text('Unable to turn off notifications. Please try again.', '关闭失败，请重试。')); }
  finally {busy=false;unsubscribeButton.disabled=false;}
 });
 setup();
})();
