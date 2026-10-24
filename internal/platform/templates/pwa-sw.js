'use strict';
// One worker per app directory. No API, console, or target-site data is cached.
const appBase = new URL('./', self.location.href);
self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', event => event.waitUntil(self.clients.claim()));
self.addEventListener('fetch', event => {
 if (event.request.mode !== 'navigate') return;
 const url = new URL(event.request.url);
 if (url.origin !== appBase.origin || !url.pathname.startsWith(appBase.pathname)) return;
 event.respondWith(fetch(event.request).catch(() => new Response('<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>You are offline</title><body><h1>You are offline</h1><p>Reconnect to the internet and open the app again.</p></body></html>',{status:503,headers:{'Content-Type':'text/html; charset=utf-8','Cache-Control':'no-store','Content-Security-Policy':"default-src 'none'; frame-ancestors 'none'"}})));
});
function safeTarget(raw) {
 try { const url=new URL(raw || 'launch',appBase); if (url.username || url.password) return new URL('launch',appBase).href; if (url.origin===appBase.origin) return url.pathname.startsWith(appBase.pathname) ? url.href : new URL('launch',appBase).href; return url.protocol==='https:' ? url.href : new URL('launch',appBase).href; } catch (_) {return new URL('launch',appBase).href;}
}
function report(data,event) {
 if (!data || typeof data.delivery_id!=='string' || typeof data.event_token!=='string') return Promise.resolve();
 return fetch(new URL('event',appBase),{method:'POST',credentials:'omit',headers:{'Content-Type':'application/json'},body:JSON.stringify({delivery_id:data.delivery_id,token:data.event_token,event}),cache:'no-store'}).catch(()=>{});
}
// Store only opaque delivery receipts, never notification text or site data.
// A retry after a provider-accepted send must not alert an already-notified
// device again. Storage can be unavailable in private mode; the stable tag is
// still used as a fallback. Do not record a receipt until display succeeds.
const receiptCache = 'd1-pwa-notification-receipts-' + appBase.pathname;
const receiptLifetime = 7 * 24 * 60 * 60 * 1000;
function receiptURL(id) {
 return typeof id === 'string' && /^[a-f0-9]{32}$/.test(id) ? new URL('notification-receipt/' + id, appBase).href : null;
}
async function wasDisplayed(id) {
 const url = receiptURL(id);
 if (!url || typeof caches === 'undefined') return false;
 try {
  const cache = await caches.open(receiptCache);
  const found = await cache.match(url);
  if (!found) return false;
  const time = Number(await found.text());
  if (Number.isFinite(time) && Date.now() - time >= 0 && Date.now() - time < receiptLifetime) return true;
  await cache.delete(url);
 } catch (_) {}
 return false;
}
async function rememberDisplayed(id) {
 const url = receiptURL(id);
 if (!url || typeof caches === 'undefined') return;
 try {
  const cache = await caches.open(receiptCache);
  await cache.put(url, new Response(String(Date.now())));
  const keys = await cache.keys();
  for (const key of keys.slice(0, Math.max(0, keys.length - 256))) await cache.delete(key);
 } catch (_) {}
}
self.addEventListener('push', event => {
 let payload={}; try {payload=event.data ? event.data.json() : {};} catch (_) {}
 const data={delivery_id:payload.delivery_id,event_token:payload.event_token,url:safeTarget(payload.url)};
 const title=typeof payload.title==='string' && payload.title ? payload.title.slice(0,200) : 'New update';
 const body=typeof payload.body==='string' ? payload.body.slice(0,2000) : '';
 // Only the app's own normalized image is used, even if a payload is malformed.
 event.waitUntil((async () => {
  if (await wasDisplayed(payload.delivery_id)) { await report(data,'displayed'); return; }
  await self.registration.showNotification(title,{body,icon:new URL('icon/192.png',appBase).href,badge:new URL('icon/192.png',appBase).href,tag:typeof payload.delivery_id==='string' ? 'd1-'+payload.delivery_id : undefined,data});
  await rememberDisplayed(payload.delivery_id);
  await report(data,'displayed');
 })());
});
self.addEventListener('notificationclick', event => {
 event.notification.close();
 const data=event.notification.data || {};
 const target=safeTarget(data.url);
 event.waitUntil(Promise.all([report(data,'clicked'),self.clients.matchAll({type:'window',includeUncontrolled:false}).then(async windows=>{
  const client=windows.find(window=>window.url===target);
  if (client) return client.focus();
  return self.clients.openWindow(target);
 })]));
});
