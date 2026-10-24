import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {runInNewContext} from 'node:vm';
import {JSDOM} from 'jsdom';
import {afterEach,expect,it,vi} from 'vitest';
const source=readFileSync(resolve(process.cwd(),'../internal/platform/templates/return-flow.js'),'utf8');
const documents:JSDOM[]=[];
function start(fetcher:ReturnType<typeof vi.fn>,options:{showOnce?:boolean;proof?:boolean;enabled?:boolean}={}){
 const dom=new JSDOM('<!doctype html><html><head><style id="pwa-return-flow-style"></style></head><body></body></html>',{url:'https://d1.example/a/test/info',pretendToBeVisual:true});documents.push(dom);const win=dom.window;const doc=win.document;Object.defineProperty(doc,'readyState',{value:'complete'});
 doc.body.dataset.returnFlow=JSON.stringify({enabled:options.enabled!==false,triggerMode:'both',showOnce:options.showOnce??false,buttonAction:'download'});doc.body.dataset.returnFlowApp='test';if(options.proof!==false){doc.body.dataset.returnFlowEndpoint='/a/test/return-flow/view';doc.body.dataset.returnFlowToken='signed-page-proof';}
 const install=vi.fn();Object.assign(win,{D1ReturnFlowInstall:install});const history={pushState:vi.fn(),back:vi.fn()};runInNewContext(source,{window:win,document:doc,history,sessionStorage:win.sessionStorage,localStorage:win.localStorage,URL,URLSearchParams,CustomEvent:win.CustomEvent,Date,Promise,console,fetch:fetcher});
 const back=()=>win.dispatchEvent(new win.Event('popstate'));const overlay=()=>doc.querySelector('.pwa-return-flow-overlay');const close=()=>doc.querySelector<HTMLButtonElement>('.pwa-return-flow-dismiss')?.click();return{win,doc,back,overlay,close,install,history};
}
afterEach(()=>{documents.forEach(d=>d.window.close());documents.length=0;});
it('authorizes one visible return page and collapses simultaneous triggers',async()=>{
 let resolveRequest!:(r:Response)=>void;const fetcher=vi.fn(()=>new Promise<Response>(resolve=>{resolveRequest=resolve;}));const c=start(fetcher);c.back();c.back();c.win.dispatchEvent(new c.win.Event('focus'));expect(fetcher).toHaveBeenCalledOnce();expect(c.overlay()).toBeNull();
 resolveRequest(new Response(JSON.stringify({allowed:true})));await vi.waitFor(()=>expect(c.overlay()).not.toBeNull());expect(fetcher).toHaveBeenCalledWith('/a/test/return-flow/view',expect.objectContaining({method:'POST',credentials:'same-origin',body:JSON.stringify({token:'signed-page-proof'})}));
 c.doc.querySelector<HTMLButtonElement>('.pwa-return-flow-action')!.click();await Promise.resolve();expect(c.install).toHaveBeenCalledWith({source:'return_flow'});expect(c.overlay()).toBeNull();
});
it('rechecks later displays and preserves show-once without additional charge requests',async()=>{
 const fetcher=vi.fn(async()=>new Response(JSON.stringify({allowed:true})));const c=start(fetcher,{showOnce:true});c.back();await vi.waitFor(()=>expect(c.overlay()).not.toBeNull());c.close();c.back();expect(fetcher).toHaveBeenCalledOnce();expect(c.overlay()).toBeNull();
 const repeat=start(fetcher);repeat.back();await vi.waitFor(()=>expect(repeat.overlay()).not.toBeNull());repeat.close();repeat.back();await vi.waitFor(()=>expect(fetcher).toHaveBeenCalledTimes(3));await vi.waitFor(()=>expect(repeat.overlay()).not.toBeNull());
});
it('does not present an unpaid overlay when authorization fails and permits a later retry',async()=>{
 const fetcher=vi.fn().mockResolvedValueOnce(new Response('{}',{status:402})).mockRejectedValueOnce(Error('offline')).mockResolvedValue(new Response(JSON.stringify({allowed:true})));const c=start(fetcher);c.back();await vi.waitFor(()=>expect(c.history.back).toHaveBeenCalledOnce());expect(c.overlay()).toBeNull();c.back();await vi.waitFor(()=>expect(c.history.back).toHaveBeenCalledTimes(2));expect(c.overlay()).toBeNull();c.back();await vi.waitFor(()=>expect(c.overlay()).not.toBeNull());
});
it('does not charge disabled, proofless or hidden pages',async()=>{
 const fetcher=vi.fn();for(const options of [{enabled:false},{proof:false}]){const c=start(fetcher,options);c.back();await Promise.resolve();expect(c.overlay()).toBeNull();}const c=start(fetcher);Object.defineProperty(c.doc,'hidden',{value:true});c.back();await Promise.resolve();expect(fetcher).not.toHaveBeenCalled();expect(c.overlay()).toBeNull();
});
