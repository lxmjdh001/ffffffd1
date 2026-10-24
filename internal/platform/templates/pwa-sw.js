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
 const fallback=new URL('launch',appBase).href;
 try {
  const url=new URL(typeof raw==='string' && raw.trim() ? raw : 'launch',appBase);
  if (url.username || url.password) return fallback;
  if (url.protocol==='https:') return url.href;
  const localOrigin=['localhost','127.0.0.1','[::1]'].includes(appBase.hostname);
  return localOrigin && url.protocol==='http:' && url.origin===appBase.origin ? url.href : fallback;
 } catch (_) {return fallback;}
}
function report(data,event) {
 if (!data || typeof data.delivery_id!=='string' || typeof data.event_token!=='string') return Promise.resolve();
 return fetch(new URL('event',appBase),{method:'POST',credentials:'omit',headers:{'Content-Type':'application/json'},body:JSON.stringify({delivery_id:data.delivery_id,token:data.event_token,event}),cache:'no-store'}).catch(()=>{});
}
self.addEventListener('push', event => {
 let payload={}; try {payload=event.data ? event.data.json() : {};} catch (_) {}
 if (!payload || typeof payload!=='object' || Array.isArray(payload)) payload={};
 const data={delivery_id:payload.delivery_id,event_token:payload.event_token,url:safeTarget(payload.url)};
 const title=typeof payload.title==='string' && payload.title ? payload.title.slice(0,200) : 'New update';
 const body=typeof payload.body==='string' ? payload.body.slice(0,2000) : '';
 // Only the app's own normalized image is used, even if a payload is malformed.
 event.waitUntil((async () => {
  // Web Push must produce a visible notification for every push event on iOS.
  // A stable delivery tag replaces an existing notification without re-alerting.
  await self.registration.showNotification(title,{body,icon:new URL('icon/192.png',appBase).href,badge:new URL('icon/192.png',appBase).href,tag:typeof payload.delivery_id==='string' ? 'd1-'+payload.delivery_id : undefined,renotify:false,data});
  await report(data,'displayed');
 })());
});
self.addEventListener('notificationclick', event => {
 event.notification.close();
 const data=event.notification.data || {};
 const target=safeTarget(data.url);
 event.waitUntil(Promise.all([report(data,'clicked'),self.clients.matchAll({type:'window',includeUncontrolled:true}).then(async windows=>{
  const client=windows.find(window=>window.url===target);
  if (client) return client.focus();
  return self.clients.openWindow(target);
 })]));
});
