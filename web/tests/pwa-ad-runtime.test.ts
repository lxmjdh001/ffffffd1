import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {runInNewContext} from 'node:vm';
import {JSDOM} from 'jsdom';
import {afterEach,expect,it,vi} from 'vitest';
const source=readFileSync(resolve(process.cwd(),'../internal/platform/templates/pwa-client.js'),'utf8');
const documents:JSDOM[]=[];
afterEach(()=>{for(const d of documents.splice(0))d.window.close();});
async function tick(){for(let i=0;i<20;i++)await Promise.resolve();}
function runtime(mode:'quick'|'deep',consent=false,standalone=false){
 const dom=new JSDOM(`<body data-base="/p/demo/" data-locale="en" data-push-enabled="false"><button id="install-button">Install app</button><p id="install-status"></p><section id="install-guide" hidden><p id="install-guide-text"></p></section><section id="d1-pwa-ads" data-mode="${mode}" data-base="/p/demo/" data-token="private-token" data-proof="proof" data-app="pwa-id" data-origin="https://customer.example" data-sdk="https://d1.example/api/sdk/pwas/pwa-id/events"><input id="d1-pwa-ad-consent" type="checkbox" ${consent?'checked':''}></section></body>`,{url:'https://d1.example/p/demo/',pretendToBeVisual:true});documents.push(dom);const w=dom.window;
 const fetcher=vi.fn(async(_url:string,_options?:RequestInit)=>new Response('{}'));
 const facade:any={isSecureContext:true,crypto:w.crypto,MutationObserver:w.MutationObserver,matchMedia:()=>({matches:standalone}),addEventListener:w.addEventListener.bind(w),removeEventListener:w.removeEventListener.bind(w),location:{assign:vi.fn()},__APP_CONFIG__:{app:{settings:{packagePriority:'pwa'}}}};
 runInNewContext(source,{window:facade,document:w.document,navigator:{userAgent:'Android Chrome',platform:'Linux',maxTouchPoints:1},localStorage:w.localStorage,fetch:fetcher,setTimeout:()=>1,clearTimeout:()=>{},URL});
 return{w,facade,fetcher};
}
it('quick setup sends on the install action without waiting for installation and never on a page view',async()=>{const f=runtime('quick');await tick();expect(f.fetcher).not.toHaveBeenCalled();await f.facade.D1ReturnFlowInstall();await tick();expect(f.fetcher).toHaveBeenCalledTimes(1);expect(f.fetcher.mock.calls[0][0]).toBe('/p/demo/ad-event');const options=f.fetcher.mock.calls[0][1] as RequestInit;expect(options.keepalive).toBe(true);expect(JSON.parse(String(options.body))).toMatchObject({event:'install_click',attribution_token:'private-token'});expect(JSON.parse(String(options.body)).consent).toBeUndefined();f.w.dispatchEvent(new f.w.Event('appinstalled'));await tick();expect(f.fetcher).toHaveBeenCalledTimes(1);});
it('quick requests are coalesced while delivery is pending and cannot stall installation UI',async()=>{const f=runtime('quick');f.fetcher.mockImplementation(()=>new Promise<Response>(()=>{}));let finished=false;await f.facade.D1ReturnFlowInstall().then(()=>finished=true);await f.facade.D1ReturnFlowInstall();expect(finished).toBe(true);expect(f.fetcher).toHaveBeenCalledTimes(1);});
it('deep setup preserves consent and requires an installation observation',async()=>{const f=runtime('deep');await f.facade.D1ReturnFlowInstall();await tick();expect(f.fetcher).not.toHaveBeenCalled();f.w.dispatchEvent(new f.w.Event('appinstalled'));await tick();expect(f.fetcher).not.toHaveBeenCalled();const box=f.w.document.querySelector<HTMLInputElement>('#d1-pwa-ad-consent')!;box.checked=true;box.dispatchEvent(new f.w.Event('change'));await tick();const events=f.fetcher.mock.calls.map(call=>JSON.parse(String((call[1] as RequestInit).body)).event);expect(events).toEqual(['consent','pwa_install']);});
it('deep first home-screen launch records installation and opening only once',async()=>{const f=runtime('deep',true,true);await tick();expect(f.fetcher.mock.calls.map(call=>JSON.parse(String((call[1] as RequestInit).body)).event)).toEqual(['pwa_install','app_open']);f.w.dispatchEvent(new f.w.Event('online'));await tick();expect(f.fetcher).toHaveBeenCalledTimes(2);});
