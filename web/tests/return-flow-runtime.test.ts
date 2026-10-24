import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { runInNewContext } from 'node:vm';
import { JSDOM } from 'jsdom';
import { afterEach, describe, expect, it, vi } from 'vitest';
const billedCandidate=readFileSync(resolve(process.cwd(),'../internal/platform/templates/return-flow.js'),'utf8');
// Compare the reference presentation/behavior after removing only D1's charge gate.
const candidate=billedCandidate.replace(/^ *\/\* D1_BILLING_START \*\/[\s\S]*?^ *\/\* D1_BILLING_END \*\/\n/gm,'');
const reference=readFileSync(resolve(process.cwd(),'tests/fixtures/pwa-return-flow.reference.js'),'utf8');
const css=readFileSync(resolve(process.cwd(),'../internal/platform/templates/return-flow.css'),'utf8');
const documents:JSDOM[]=[];
type Options={path?:string;query?:string;zh?:boolean;blocked?:boolean;storage?:Map<string,string>};
function start(kind:'reference'|'candidate',config:Record<string,unknown>={},options:Options={}){
 const path=options.path||'/p/0123456789abcdef/';const query=options.query||'';const origin='https://d1.example';
 const dom=new JSDOM('<!doctype html><html><head><style id="pwa-return-flow-style"></style></head><body><main>Installation page</main></body></html>',{url:origin+path+query,pretendToBeVisual:true});documents.push(dom);const real=dom.window;const document=real.document;Object.defineProperty(document,'readyState',{value:'complete'});document.documentElement.lang=options.zh?'zh-CN':'en';const raw={enabled:true,triggerMode:'both',showOnce:true,...config};document.body.dataset.returnFlow=JSON.stringify(raw);document.body.dataset.returnFlowApp='app-one';
 const storage=options.storage||new Map<string,string>();const sessionStorage={getItem:(key:string)=>{if(options.blocked)throw Error('blocked');return storage.get(key)||null;},setItem:(key:string,value:string)=>{if(options.blocked)throw Error('blocked');storage.set(key,value);}};
 const history={pushState:vi.fn(),back:vi.fn()};const navigations:string[]=[];let href=origin+path+query;const location={origin,host:'d1.example',pathname:path,search:query,protocol:'https:',get href(){return href;},set href(value:string){href=value;navigations.push(value);}};
 const install=vi.fn().mockResolvedValue(undefined);const reports:unknown[]=[];const events:unknown[]=[];let timestamp=1000;
 const appConfig={app:{id:'app-one',storeInfo:{appName:'North Studio'},settings:{packagePriority:'pwa',returnFlowConfig:raw}},s3PublicDomain:origin};
 const facade={location,history,__APP_CONFIG__:appConfig,D1ReturnFlowInstall:install,installPWA:install,scheduleReturnFlowRiskCancelPrompt:(_reason?:string)=>false,addEventListener:real.addEventListener.bind(real),removeEventListener:real.removeEventListener.bind(real),dispatchEvent:real.dispatchEvent.bind(real)};
 real.addEventListener('d1-return-flow-track',event=>{const detail=(event as CustomEvent).detail;reports.push([detail.type,{contentName:detail.contentName}]);});
 for(const name of ['view','click'])real.addEventListener('pwa-return-flow-'+name,event=>events.push([name,(event as CustomEvent).detail]));
 const messages:Record<string,string[]>={returnFlowBadge:['Limited offer','限时福利'],returnFlowTitle:['Your exclusive benefit is ready','专属福利已准备好'],returnFlowDescription:['Come back to claim it. Tap the button to continue.','回来就能领取，点击按钮马上继续。'],returnFlowButton:['Claim now','立即领取'],returnFlowDismiss:['Maybe later','稍后再看']};
 const globals={window:facade,document,history,sessionStorage,localStorage:{getItem:()=>null},URL,URLSearchParams,CustomEvent:real.CustomEvent,Promise,Date:{now:()=>timestamp},console,t:(key:string)=>messages[key][options.zh?1:0],getLinkCode:()=>new URLSearchParams(query).get('linkCode')||new URLSearchParams(query).get('link_code')||null,resolveSafeDownloadAssetUrl:(value:string)=>!value?'':/^(https?:)?\/\//i.test(value)||value.startsWith('data:')||value.startsWith('/')?value:origin+'/'+value.replace(/^\/+/,''),trackEvent:async(type:string,details:unknown)=>{reports.push([type,details]);}};
 runInNewContext(kind==='candidate'?candidate:reference+'\ninitReturnFlow();',globals);
 const event=(name:string,detail?:unknown)=>real.dispatchEvent(new real.CustomEvent(name,{detail}));
 const hidden=(value:boolean)=>{timestamp++;Object.defineProperty(document,'hidden',{value,configurable:true});document.dispatchEvent(new real.Event('visibilitychange'));};
 const click=(selector:string)=>(document.querySelector(selector) as HTMLButtonElement)?.click();
 const snapshot=()=>({overlay:document.getElementById('pwa-return-flow-overlay')?.outerHTML||null,locked:document.documentElement.classList.contains('pwa-return-flow-lock'),storage:[...storage.entries()],push:history.pushState.mock.calls,back:history.back.mock.calls.length,navigations,install:install.mock.calls,events:JSON.parse(JSON.stringify(events)),reports:JSON.parse(JSON.stringify(reports))});
 return{document,real,history,install,facade,event,hidden,click,snapshot};
}
afterEach(()=>{for(const dom of documents)dom.window.close();documents.length=0;vi.restoreAllMocks();});
async function compare(config:Record<string,unknown>,scenario:(c:ReturnType<typeof start>)=>void|Promise<void>,options:Options={}){const a=start('reference',config,options);const b=start('candidate',config,{...options,storage:options.storage?new Map(options.storage):undefined});await scenario(a);await scenario(b);expect(b.snapshot()).toEqual(a.snapshot());return b;}

describe('Return page is restored from the original PWA',()=>{
 it('preserves injected landing store content and installation settings',()=>{const c=start('candidate');expect(c.facade.__APP_CONFIG__.app.storeInfo.appName).toBe('North Studio');expect(c.facade.__APP_CONFIG__.app.settings.packagePriority).toBe('pwa');});

 it('contains the original behavior block verbatim and exactly the original CSS',()=>{expect(candidate).toContain(reference.trimEnd());const original=reference.match(/style\.textContent = `([\s\S]*?)`;\n    document.head.appendChild\(style\);/)![1];expect(css.trim()).toBe(original.trim());});
 it('matches initial foreground and disabled behavior',async()=>{await compare({},c=>{c.event('focus');c.event('pageshow');});await compare({enabled:false},c=>{c.event('popstate');c.hidden(true);c.hidden(false);});});
 it('matches Back, gesture rearming, close and subsequent navigation',async()=>{await compare({triggerMode:'back'},c=>{c.event('pointerdown');c.event('mousedown');c.event('popstate');c.click('.pwa-return-flow-dismiss');c.event('popstate');});});
 it('matches repeated Back when showOnce is off',async()=>{await compare({triggerMode:'back',showOnce:false},c=>{c.event('popstate');c.click('.pwa-return-flow-dismiss');c.event('popstate');});});
 it('matches foreground deduplication and another resume cycle',async()=>{await compare({triggerMode:'resume',showOnce:false},c=>{c.hidden(true);c.hidden(false);c.event('focus');c.event('pageshow');c.click('.pwa-return-flow-dismiss');c.hidden(true);c.hidden(false);});});
 it('matches one-shot behavior and its original per-link storage scope',async()=>{const c=await compare({},c=>{c.hidden(true);c.hidden(false);c.click('.pwa-return-flow-dismiss');c.hidden(true);c.hidden(false);},{query:'?link_code=campaign-one'});expect(c.snapshot().storage.some(([key])=>key.endsWith(':campaign-one:app-one'))).toBe(true);});
 it('matches the source with blocked session storage',async()=>{await compare({},c=>{c.hidden(true);c.hidden(false);c.click('.pwa-return-flow-dismiss');c.hidden(true);c.hidden(false);},{blocked:true});});
 it('matches download interruption, risk prompt scheduling and focus deduplication',async()=>{await compare({showOnDownloadRiskCancel:true},c=>{expect(c.facade.scheduleReturnFlowRiskCancelPrompt('download_risk_cancel')).toBe(true);c.event('focus');c.event('blur');c.event('focus');c.event('pageshow');});});
 it('matches visibility-based download return and generic resume ordering',async()=>{await compare({showOnDownloadRiskCancel:true},c=>{c.facade.scheduleReturnFlowRiskCancelPrompt();c.hidden(true);c.hidden(false);c.event('focus');});});
 it('matches the explicit download/install action and source parameter',async()=>{const c=await compare({buttonAction:'download'},async c=>{c.event('popstate');c.click('.pwa-return-flow-action');await Promise.resolve();});expect(c.install).toHaveBeenCalledWith({source:'return_flow'});});
 it.each(['https://shop.example/offer','http://shop.example/offer','//shop.example/offer','/offer','shop.example/offer'])('matches URL normalization for %s',async buttonUrl=>{await compare({buttonAction:'url',buttonUrl},c=>{c.event('popstate');c.click('.pwa-return-flow-action');});});
 it('matches Continue with a legacy URL and URL action without a destination',async()=>{await compare({buttonAction:'continue',buttonUrl:'shop.example/offer'},c=>{c.event('popstate');c.click('.pwa-return-flow-action');});await compare({buttonAction:'url',buttonUrl:''},c=>{c.event('popstate');c.click('.pwa-return-flow-action');});});
 it.each([50,75,100])('matches image markup and media area %i',async mediaAreaPercent=>{await compare({mediaUrl:'https://media.example/banner.gif',mediaAreaPercent},c=>c.event('popstate'));});
 it('matches silent looping inline video and Chinese fallback text',async()=>{const c=await compare({mediaUrl:'https://media.example/creative.mp4',mediaAreaPercent:100},c=>c.event('popstate'),{zh:true});const video=c.document.querySelector('video')!;expect(video.muted&&video.loop&&video.autoplay&&video.playsInline).toBe(true);expect(c.document.querySelector('.pwa-return-flow-title')?.textContent).toBe('专属福利已准备好');});
 it('matches no-media layout and treats custom text as literal text',async()=>{const c=await compare({title:'<img onerror="alert(1)">',description:'<script>alert(1)</script>'},c=>c.event('popstate'));expect(c.document.querySelector('script')).toBeNull();});
 it('connects D1 APK download events without recursively scheduling return-flow downloads',()=>{const c=start('candidate',{triggerMode:'back',showOnce:false,showOnDownloadRiskCancel:true});c.event('d1-apk-download-start',{source:'return_flow'});c.event('blur');c.event('focus');expect(c.snapshot().overlay).toBeNull();c.event('d1-apk-download-start',{source:'page'});c.event('blur');c.event('focus');expect(c.snapshot().overlay).toBeTruthy();});
});
