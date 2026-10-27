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
function safeTarget(raw,fallback=new URL('launch',appBase).href) {
 try {
  if (typeof raw!=='string' || !raw.trim()) return fallback;
  const url=new URL(raw,appBase);
  if (url.username || url.password) return fallback;
  if (url.protocol==='https:') return url.href;
  const localOrigin=['localhost','127.0.0.1','[::1]'].includes(appBase.hostname);
  return localOrigin && url.protocol==='http:' && url.origin===appBase.origin ? url.href : fallback;
 } catch (_) {return fallback;}
}
function safeImage(raw) {
 if (typeof raw!=='string' || !raw.trim() || raw.length>2048) return undefined;
 // Notification images are loaded by the browser; they never pass through the server.
 return safeTarget(raw,'') || undefined;
}
function notificationActions(raw,mainURL) {
 if (!Array.isArray(raw)) return [];
 let limit=2;
 if (typeof Notification!=='undefined' && Number.isInteger(Notification.maxActions) && Notification.maxActions>=0) limit=Math.min(limit,Notification.maxActions);
 const actions=[];
 const seen=new Set();
 for (const item of raw) {
  if (actions.length>=limit) break;
  if (!item || typeof item!=='object' || typeof item.action!=='string' || !/^[A-Za-z0-9_-]{1,50}$/.test(item.action) || typeof item.title!=='string' || !item.title.trim()) continue;
  if (seen.has(item.action)) continue;
  seen.add(item.action);
  actions.push({action:item.action,title:Array.from(item.title).slice(0,40).join(''),url:safeTarget(item.url,mainURL)});
 }
 return actions;
}
function report(data,event) {
 if (!data || typeof data.delivery_id!=='string' || typeof data.event_token!=='string') return Promise.resolve();
 return fetch(new URL('event',appBase),{method:'POST',credentials:'omit',headers:{'Content-Type':'application/json'},body:JSON.stringify({delivery_id:data.delivery_id,token:data.event_token,event}),cache:'no-store'}).catch(()=>{});
}
self.addEventListener('push', event => {
 let payload={}; try {payload=event.data ? event.data.json() : {};} catch (_) {}
 if (!payload || typeof payload!=='object' || Array.isArray(payload)) payload={};
 const mainURL=safeTarget(payload.url);
 const actions=notificationActions(payload.actions,mainURL);
 const data={delivery_id:payload.delivery_id,event_token:payload.event_token,url:mainURL,actions:Object.fromEntries(actions.map(item=>[item.action,item.url]))};
 const title=typeof payload.title==='string' && payload.title ? payload.title.slice(0,200) : 'New update';
 const body=typeof payload.body==='string' ? payload.body.slice(0,2000) : '';
 const appIcon=new URL('/pwa-template-assets/push-google.png',appBase).href;
 const image=safeImage(payload.image);
 const options={body,icon:appIcon,badge:appIcon,tag:typeof payload.delivery_id==='string' ? 'd1-'+payload.delivery_id : undefined,renotify:false,data};
 if (image) options.image=image;
 if (actions.length) options.actions=actions.map(item=>({action:item.action,title:item.title}));
 event.waitUntil((async () => {
  // Web Push must produce a visible notification for every push event on iOS.
  // A stable delivery tag replaces an existing notification without re-alerting.
  try {
   await self.registration.showNotification(title,options);
  } catch (error) {
   // Older browsers may reject rich options instead of ignoring them.
   if (!error || error.name!=='TypeError' || (!options.image && !options.actions) || (typeof Notification!=='undefined' && Notification.permission==='denied')) throw error;
   const basic={...options,data:{...data,actions:{}}};
   delete basic.image;
   delete basic.actions;
   await self.registration.showNotification(title,basic);
  }
  await report(data,'displayed');
 })());
});
self.addEventListener('notificationclick', event => {
 event.notification.close();
 const data=event.notification.data || {};
 const mainURL=safeTarget(data.url);
 const action=typeof event.action==='string' ? event.action : '';
 const urls=data.actions;
 const target=action && urls && typeof urls==='object' && !Array.isArray(urls) && Object.prototype.hasOwnProperty.call(urls,action) ? safeTarget(urls[action],mainURL) : mainURL;
 event.waitUntil(Promise.all([report(data,'clicked'),self.clients.matchAll({type:'window',includeUncontrolled:true}).then(async windows=>{
  const client=windows.find(window=>window.url===target);
  if (client) return client.focus();
  return self.clients.openWindow(target);
 })]));
});
