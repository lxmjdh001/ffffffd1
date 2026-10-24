import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {runInNewContext} from 'node:vm';
import {JSDOM} from 'jsdom';
import {afterEach, describe, expect, it, vi} from 'vitest';

const client=readFileSync(resolve(process.cwd(),'../internal/platform/templates/pwa-client.js'),'utf8');
const appBase='/p/0123456789abcdef/';
const documents:JSDOM[]=[];
type Options={layout?:'google_play'|'custom'|'legacy';origin?:string;base?:string;cached?:boolean;standalone?:boolean;storageBlocked?:boolean;runtime?:Record<string,any>;lateMount?:boolean;browserGuide?:()=>boolean;storage?:Record<string,string>;billing?:boolean;billingFailure?:boolean;completeFailure?:boolean};
function start(options:Options={}){
 const layout=options.layout||'google_play',base=options.base||appBase,origin=options.origin||'https://chosen.example';
 const controls=layout==='legacy'?'<button id="install-button">Install app <span>↓</span></button>':layout==='custom'?'<button class="install-button">Install now</button>':'<button class="install-button">Install <svg aria-hidden="true"></svg></button><button class="stay-prompt-install">Install</button>';
 const body=`<body ${layout==='legacy'?'':'data-d1-pwa-reference'} data-base="${base}" data-locale="en" data-push-enabled="false">${layout==='legacy'?controls:`<div id="app">${options.lateMount?'':controls}</div>`}<p id="install-status"></p><section id="install-guide" hidden><p id="install-guide-text"></p></section><button id="notification-button">Enable notifications</button><p id="notification-status"></p><button id="other">Other action</button></body>`;
 const dom=new JSDOM(body,{url:origin+'/c/chosen-cloak',pretendToBeVisual:true});documents.push(dom);
 const win=dom.window,document=win.document,key='d1-pwa-installed:'+base;
 if(options.billing)document.body.dataset.returnFlowToken='signed-proof';
 for(const [k,v] of Object.entries(options.storage||{}))win.localStorage.setItem(k,v);
 if(options.cached)win.localStorage.setItem(key,'true');
 const opened:{url:string;target:string;rel:string}[]=[];
 vi.spyOn(win.HTMLAnchorElement.prototype,'click').mockImplementation(function(this:HTMLAnchorElement){opened.push({url:this.href,target:this.target,rel:this.rel});});
 let completionFailures=options.completeFailure?1:0;
 const location={assign:vi.fn()},permission=vi.fn(),fetcher=vi.fn(async(url:string)=>{
  if(options.billingFailure&&url.endsWith('/start'))return new Response(JSON.stringify({error:'Insufficient credit'}),{status:402});
  if(url.endsWith('/complete')&&completionFailures-->0)throw Error('offline');
  return new Response(JSON.stringify({state:url.endsWith('/start')?'pending':url.endsWith('/complete')?'charged':'released'}));
 });
 const runtime=options.runtime||{app:{settings:{packagePriority:'pwa'}}};
 const facade:Record<string,any>={crypto:win.crypto,isSecureContext:true,matchMedia:(query:string)=>({matches:!!options.standalone&&query==='(display-mode: standalone)'}),MutationObserver:win.MutationObserver,addEventListener:win.addEventListener.bind(win),removeEventListener:win.removeEventListener.bind(win),__APP_CONFIG__:runtime,location,showBrowserGuide:options.browserGuide};
 const storage=options.storageBlocked?{getItem:()=>{throw Error('blocked');},setItem:()=>{throw Error('blocked');},removeItem:()=>{throw Error('blocked');}}:win.localStorage;
 let nextTimer=0;const timers=new Map<number,{callback:()=>void;delay:number}>();
 const schedule=vi.fn((callback:()=>void,delay:number)=>{const id=++nextTimer;timers.set(id,{callback,delay});return id;});
 const cancel=vi.fn((id:number)=>{timers.delete(id);});
 runInNewContext(client,{window:facade,document,navigator:{userAgent:'Android Chrome',platform:'Linux',maxTouchPoints:1},localStorage:storage,Notification:{permission:'default',requestPermission:permission},fetch:fetcher,setTimeout:schedule,clearTimeout:cancel});
 const button=()=>document.querySelector<HTMLButtonElement>(layout==='legacy'?'#install-button':'#app .install-button')!;
 const offer=(outcome='dismissed',choice?:Promise<{outcome:string}>)=>{
  const event=new win.Event('beforeinstallprompt',{cancelable:true});
  const prompt=vi.fn().mockResolvedValue(undefined);
  Object.assign(event,{prompt,userChoice:choice||Promise.resolve({outcome})});
  win.dispatchEvent(event);expect(event.defaultPrevented).toBe(true);return prompt;
 };
 return {dom,win,document,facade,location,runtime,opened,permission,fetcher,key,button,offer,timers,animating:()=>document.body.hasAttribute('data-d1-pwa-installing'),expireAnimation:()=>{for(const [id,timer] of timers)if(timer.delay===60000){timers.delete(id);timer.callback();}},confirm:()=>win.dispatchEvent(new win.Event('appinstalled')),install:(options?:Record<string,unknown>)=>facade.D1ReturnFlowInstall(options),flush:()=>new Promise<void>(resolve=>win.setTimeout(resolve,0))};
}
afterEach(()=>{for(const dom of documents)dom.window.close();documents.length=0;vi.restoreAllMocks();});

describe('PWA Install to Play',()=>{
 it('reserves before prompting, charges only appinstalled, and retries with the same event ID',async()=>{
  const c=start({billing:true,completeFailure:true});const prompt=c.offer('accepted');await c.install();
  expect(prompt).toHaveBeenCalledOnce();expect(c.fetcher).toHaveBeenCalledTimes(1);expect(c.fetcher.mock.calls[0][0]).toBe(appBase+'install/start');expect(c.button().textContent).toContain('Install');
  c.confirm();await vi.waitFor(()=>expect(c.fetcher).toHaveBeenCalledTimes(2));expect(c.button().textContent).toContain('Play');
  c.win.dispatchEvent(new c.win.Event('online'));await vi.waitFor(()=>expect(c.fetcher).toHaveBeenCalledTimes(3));
  const calls=c.fetcher.mock.calls as unknown as [string,RequestInit][];const bodies=calls.map(([,r])=>JSON.parse(String(r.body)));expect(bodies[0]).toEqual({key:expect.stringMatching(/^[a-f0-9]{32}$/),token:'signed-proof'});expect(bodies[1]).toEqual(bodies[0]);expect(bodies[2]).toEqual(bodies[0]);
  c.confirm();await c.flush();expect(c.fetcher).toHaveBeenCalledTimes(3);
  // A fresh browser offer represents a separate installation, not a retry.
  c.offer('accepted');await c.install();c.confirm();await vi.waitFor(()=>expect(c.fetcher).toHaveBeenCalledTimes(5));expect(JSON.parse(String(calls[3][1].body)).key).not.toBe(bodies[0].key);
 });
 it('releases cancelled installation without completion or Play',async()=>{
  const c=start({billing:true});c.offer('dismissed');await c.install();expect(c.fetcher.mock.calls.map(([url])=>url)).toEqual([appBase+'install/start',appBase+'install/cancel']);expect(c.button().textContent).toContain('Install');expect(c.win.localStorage.getItem(c.key)).toBeNull();
 });
 it('shows insufficient credit, keeps the native prompt available, and never starts installation',async()=>{
  const c=start({billing:true,billingFailure:true}),prompt=c.offer('accepted');await c.install();expect(prompt).not.toHaveBeenCalled();expect(c.document.getElementById('install-guide')!.hidden).toBe(false);expect(c.document.getElementById('install-guide-text')!.textContent).toBe('Insufficient credit');expect(c.button().textContent).toContain('Install');
  await c.install();expect(prompt).not.toHaveBeenCalled();expect(c.fetcher.mock.calls.filter(([url])=>url.endsWith('/start'))).toHaveLength(2);
 });
 it.each(['google_play','custom','legacy'] as const)('%s switches only on confirmed installation and opens the selected domain',async layout=>{
  const c=start({layout,origin:'https://domain-b.example'}),button=c.button(),before=button.textContent,icon=button.querySelector('svg,span');
  const prompt=c.offer('accepted');await c.install();
  expect(prompt).toHaveBeenCalledOnce();expect(button.textContent).toBe(before);expect(c.win.localStorage.getItem(c.key)).toBeNull();
  c.confirm();expect(button.childNodes[0].nodeValue).toBe('Play');expect(button.getAttribute('aria-label')).toBe('Play');expect(button.disabled).toBe(false);
  expect(button.querySelector('svg,span')).toBe(icon);expect(c.win.localStorage.getItem(c.key)).toBe('true');
  const originalClick=vi.fn();button.addEventListener('click',originalClick);button.click();
  expect(originalClick).not.toHaveBeenCalled();expect(c.opened).toEqual([{url:'https://domain-b.example'+appBase+'launch',target:'_blank',rel:'noopener'}]);
  expect(c.location.assign).not.toHaveBeenCalled();expect(prompt).toHaveBeenCalledOnce();expect(c.permission).not.toHaveBeenCalled();expect(c.fetcher).not.toHaveBeenCalled();
  expect(c.document.getElementById('notification-button')!.textContent).toBe('Enable notifications');
 });
 it.each(['google_play','custom','legacy'] as const)('%s cancellation keeps the original button and does not save an installed marker',async layout=>{
  const c=start({layout}),original=c.button().textContent;c.offer('dismissed');await c.install();
  expect(c.button().textContent).toBe(original);expect(c.button().disabled).toBe(false);expect(c.button().hasAttribute('data-d1-pwa-action')).toBe(false);
  expect(c.win.localStorage.getItem(c.key)).toBeNull();expect(c.document.getElementById('install-status')!.textContent).toContain('canceled');expect(c.opened).toEqual([]);
  const existingClick=vi.fn();c.button().addEventListener('click',existingClick);c.button().click();expect(existingClick).toHaveBeenCalledOnce();
 });
 it('does not switch for accepted or failed prompts without an installation event',async()=>{
  const c=start();c.offer('accepted');await c.install();expect(c.button().getAttribute('aria-label')).not.toBe('Play');
  const prompt=c.offer();prompt.mockRejectedValueOnce(Error('failed'));await c.install();
  expect(c.document.getElementById('install-guide')!.hidden).toBe(false);expect(c.button().textContent).toContain('Install');expect(c.win.localStorage.getItem(c.key)).toBeNull();
 });
 it('keeps a confirmed success when userChoice resolves later and prevents duplicate prompts',async()=>{
  const c=start();let complete!:(choice:{outcome:string})=>void;
  const choice=new Promise<{outcome:string}>(resolve=>{complete=resolve;});const prompt=c.offer('accepted',choice),pending=c.install();
  await Promise.resolve();expect(c.button().disabled).toBe(true);expect(await c.install()).toEqual({outcome:'pending'});expect(prompt).toHaveBeenCalledOnce();
  c.confirm();expect(c.button().disabled).toBe(false);complete({outcome:'dismissed'});await pending;
  expect(c.button().textContent).toContain('Play');expect(c.document.getElementById('install-status')!.textContent).toContain('Tap Play');
 });
 it('restores the last confirmed hint per app, and a new install offer clears a stale hint',async()=>{
  const c=start({cached:true});expect(c.button().textContent).toContain('Play');
  const storage={[c.key]:'true'};const other=start({base:'/p/other-app/',storage});expect(other.button().textContent).toContain('Install');
  c.offer();expect(c.button().textContent).toContain('Install');expect(c.button().hasAttribute('aria-label')).toBe(false);expect(c.win.localStorage.getItem(c.key)).toBeNull();
 });
 it('handles late Vue mounts, text replacement and locale changes without changing unrelated controls',async()=>{
  const c=start({cached:true,lateMount:true});const app=c.document.getElementById('app')!;
  app.innerHTML='<button class="install-button">Install now<svg></svg></button>';await c.flush();expect(c.button().firstChild!.nodeValue).toBe('Play');
  const svg=c.button().querySelector('svg');c.button().firstChild!.nodeValue='Installer ';await c.flush();expect(c.button().firstChild!.nodeValue).toBe('Play');expect(c.button().querySelector('svg')).toBe(svg);
  c.offer();expect(c.button().firstChild!.nodeValue).toBe('Installer ');expect(c.document.getElementById('other')!.textContent).toBe('Other action');
 });
 it('works in memory when browser storage is blocked, and opens inside an existing app window',async()=>{
  const blocked=start({storageBlocked:true});blocked.confirm();expect(blocked.button().textContent).toContain('Play');blocked.button().click();expect(blocked.opened).toHaveLength(1);
  const standalone=start({standalone:true});expect(standalone.button().textContent).toContain('Play');standalone.button().click();
  expect(standalone.location.assign).toHaveBeenCalledExactlyOnceWith(appBase+'launch');expect(standalone.opened).toEqual([]);
 });
 it('does not turn APK-only downloads into Play, including custom template settings',async()=>{
  const c=start({layout:'custom',runtime:{customApp:{otherSettings:{packagePriority:'w2a_only',apkUrl:'https://apk.example/app.apk'}}}});c.confirm();
  expect(c.button().textContent).toBe('Install now');expect(await c.install()).toEqual({outcome:'apk_download'});expect(c.opened[0].url).toBe('https://apk.example/app.apk');expect(c.opened[0].target).toMatch(/^d1-pwa-download-/);
 });
 it('keeps browser-open guidance and the return-flow installation entry working',async()=>{
  const guide=vi.fn(()=>true),c=start({cached:true,browserGuide:guide});c.button().click();expect(guide).toHaveBeenCalledOnce();expect(c.opened).toEqual([]);
  guide.mockReturnValue(false);expect(await c.install({source:'return_flow'})).toEqual({outcome:'opened'});expect(c.opened[0].url).toBe('https://chosen.example'+appBase+'launch');
 });
});

describe('PWA icon-only installation animation',()=>{
 it.each(['google_play','legacy'] as const)('%s animates while prompting and awaiting confirmation without changing the label or icon markup',async layout=>{
  const c=start({layout}),original=c.button().textContent;
  const icon=c.document.createElement('img');icon.className=layout==='legacy'?'app-icon':'app-icon-large';c.document.body.appendChild(icon);
  let choose!:(value:{outcome:string})=>void;
  c.offer('accepted',new Promise(resolve=>{choose=resolve;}));const installing=c.install();
  expect(c.animating()).toBe(true);expect(c.button().textContent).toBe(original);expect(c.button().disabled).toBe(true);
  expect(icon.className).toBe(layout==='legacy'?'app-icon':'app-icon-large');expect(icon.parentElement).toBe(c.document.body);
  await Promise.resolve();choose({outcome:'accepted'});await installing;
  expect(c.animating()).toBe(true);expect(c.button().textContent).toBe(original);expect(c.button().disabled).toBe(false);
  c.confirm();expect(c.animating()).toBe(false);expect(c.timers.size).toBe(0);expect(c.button().textContent).toContain('Play');
 });
 it('stops on cancellation or an exception and leaves the original install button intact',async()=>{
  const c=start(),original=c.button().textContent;c.offer();await c.install();
  expect(c.animating()).toBe(false);expect(c.timers.size).toBe(0);expect(c.button().textContent).toBe(original);
  const prompt=c.offer();prompt.mockRejectedValueOnce(Error('install failed'));await c.install();
  expect(c.animating()).toBe(false);expect(c.timers.size).toBe(0);expect(c.button().textContent).toBe(original);expect(c.button().disabled).toBe(false);
 });
 it('bounds a missing success signal without faking Play, and still accepts a late success',async()=>{
  const c=start();c.offer('accepted');await c.install();expect(c.animating()).toBe(true);
  c.expireAnimation();expect(c.animating()).toBe(false);expect(c.button().textContent).toContain('Install');expect(c.win.localStorage.getItem(c.key)).toBeNull();
  c.confirm();expect(c.button().textContent).toContain('Play');expect(c.animating()).toBe(false);
 });
 it('does not start for a manual guide, browser-open guide, APK-only download or Play',async()=>{
  const manual=start();await manual.install();expect(manual.animating()).toBe(false);expect(manual.timers.size).toBe(0);
  const guide=start({browserGuide:()=>true});guide.offer('accepted');await guide.install();expect(guide.animating()).toBe(false);
  const apk=start({runtime:{app:{settings:{packagePriority:'w2a_only',apkUrl:'https://apk.example/app.apk'}}}});apk.offer('accepted');await apk.install();expect(apk.animating()).toBe(false);
  const play=start({cached:true});await play.install();expect(play.animating()).toBe(false);expect(play.timers.size).toBe(0);
 });
 it('keeps a later offer and cancellation from being overwritten by an older prompt result',async()=>{
  const c=start();let choose!:(value:{outcome:string})=>void;
  c.offer('accepted',new Promise(resolve=>{choose=resolve;}));const old=c.install();await Promise.resolve();
  c.offer('dismissed');expect(c.animating()).toBe(false);await c.install();
  choose({outcome:'accepted'});await old;
  expect(c.animating()).toBe(false);expect(c.button().textContent).toContain('Install');expect(c.document.getElementById('install-status')!.textContent).toContain('canceled');
 });
 it('uses scoped CSS transforms and decorative rings, not button animations or new DOM wrappers',()=>{
  const css=readFileSync(resolve(process.cwd(),'../internal/platform/templates/pwa-install-animation.css'),'utf8');
  expect(css).toContain('body[data-d1-pwa-installing] #app .app-icon-large');expect(css).toContain('transform: scale(.68)');
  expect(css).toContain('border-top-color: #4285f4');expect(css).toContain('pointer-events: none');expect(css).toContain('prefers-reduced-motion: reduce');
  expect(css).not.toContain('.install-button');expect(css).not.toContain('#install-button');
 });
});
