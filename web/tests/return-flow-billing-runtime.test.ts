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
it('shows a return page immediately without authorization, debit, or completion requests',async()=>{
 const fetcher=vi.fn().mockRejectedValue(Error('offline'));const c=start(fetcher);c.back();expect(c.overlay()).not.toBeNull();c.back();c.win.dispatchEvent(new c.win.Event('focus'));expect(c.doc.querySelectorAll('.pwa-return-flow-overlay')).toHaveLength(1);expect(fetcher).not.toHaveBeenCalled();
 c.doc.querySelector<HTMLButtonElement>('.pwa-return-flow-action')!.click();await Promise.resolve();expect(c.install).toHaveBeenCalledWith({source:'return_flow'});expect(c.overlay()).toBeNull();c.win.dispatchEvent(new c.win.Event('online'));expect(fetcher).not.toHaveBeenCalled();
});
it('preserves show-once and repeated free displays without requiring a payment proof',async()=>{
 const fetcher=vi.fn();const once=start(fetcher,{showOnce:true,proof:false});once.back();expect(once.overlay()).not.toBeNull();once.close();once.back();expect(once.overlay()).toBeNull();
 const repeat=start(fetcher,{proof:false});for(let n=0;n<3;n++){repeat.back();expect(repeat.overlay()).not.toBeNull();repeat.close();}expect(fetcher).not.toHaveBeenCalled();
});
it('never shows disabled or hidden return pages',()=>{
 const fetcher=vi.fn();const disabled=start(fetcher,{enabled:false});disabled.back();expect(disabled.overlay()).toBeNull();const hidden=start(fetcher);Object.defineProperty(hidden.doc,'hidden',{value:true});hidden.back();expect(hidden.overlay()).toBeNull();expect(fetcher).not.toHaveBeenCalled();
});
