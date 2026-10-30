import {readFileSync} from 'node:fs';
import {resolve} from 'node:path';
import {runInNewContext} from 'node:vm';
import {JSDOM} from 'jsdom';
import {afterEach,expect,it,vi} from 'vitest';
const source=readFileSync(resolve('../internal/platform/download.go'),'utf8');
const start=source.indexOf("const form=document.getElementById('d1-native-download')");
const end=source.indexOf('` + "\\n" + returnFlowJS',start);
const script=source.slice(source.indexOf('function downloadCopy()'),end);
const messages=JSON.parse(readFileSync(resolve('../internal/platform/templates/page-locales.json'),'utf8'));
const windows:JSDOM[]=[];
afterEach(()=>{for(const dom of windows)dom.window.close();windows.length=0;});
function runtime(traffic:boolean,payload:unknown,statusCode=200,language='en',configuredLanguage?:string){
 const dom=new JSDOM(`<form id="d1-native-download" action="/a/demo/${traffic?'button':'download'}" ${traffic?'data-traffic-button':''}><input name="request_key" value="${'a'.repeat(32)}"><button>Download</button></form><p id="download-status"></p>`,{url:'https://chosen.example/a/demo/info'});windows.push(dom);const win=dom.window;win.document.documentElement.lang=language;
 const assign=vi.fn(),fetcher=vi.fn(async()=>new Response(JSON.stringify(payload),{status:statusCode}));
 const facade={location:{assign},dispatchEvent:vi.fn(),__APP_CONFIG__:configuredLanguage?{app:{storeInfo:{language:configuredLanguage}}}:undefined};
 runInNewContext(script,{window:facade,document:win.document,languages:messages,URLSearchParams,FormData:win.FormData,CustomEvent:win.CustomEvent,fetch:fetcher});
 const form=win.document.querySelector('form')!;
 form.dispatchEvent(new win.Event('submit',{cancelable:true}));
 return {assign,fetcher,status:()=>win.document.getElementById('download-status')!.textContent,statusElement:win.document.getElementById('download-status')!,button:form.querySelector('button')!};
}
it('uses a routing result for the bound button without requesting the original APK',async()=>{
 const r=runtime(true,{redirect_url:'https://destination.example/p/app/'});await vi.waitFor(()=>expect(r.assign).toHaveBeenCalledWith('https://destination.example/p/app/'));
 expect(r.status()).toBe('');expect(r.fetcher).toHaveBeenCalledOnce();expect(r.fetcher.mock.calls[0][0]).toBe('https://chosen.example/a/demo/button');expect(r.button.disabled).toBe(false);
});
it.each([[false,'https://destination.example'],[true,'javascript:alert(1)'],[true,'//other.example']])('rejects an unexpected or unsafe routing result (%s, %s)',async(traffic,target)=>{
 const r=runtime(traffic,{redirect_url:target});await vi.waitFor(()=>expect(r.status()).toBe(messages.en.invalid_url));expect(r.assign).not.toHaveBeenCalled();
});
it('shows unavailable routing instead of silently downloading the original file',async()=>{
 const r=runtime(true,{error:'No safe target available'},503);await vi.waitFor(()=>expect(r.status()).toBe(messages.en.download_failed));expect(r.assign).not.toHaveBeenCalled();expect(r.fetcher).toHaveBeenCalledOnce();
});
it.each([false,true])('keeps native download results working, including an old page after unbinding (%s)',async traffic=>{
 const path='/d/'+'b'.repeat(64),r=runtime(traffic,{download_url:path});await vi.waitFor(()=>expect(r.assign).toHaveBeenCalledWith(path));
 expect(r.status()).toBe('');expect(r.statusElement.hidden).toBe(true);
});
it.each([['ar-SA','ar-SA'],['ar_EG','ar-SA'],['en-US','en'],['de-DE','de'],['unsupported','en']])('localizes failures for %s and never exposes raw server messages',async(language,key)=>{
 const r=runtime(true,{error:'服务暂时不可用'},503,language);await vi.waitFor(()=>expect(r.status()).toBe(messages[key].download_failed));expect(r.statusElement.hidden).toBe(false);expect(r.assign).not.toHaveBeenCalled();
});
it('uses the publisher-selected language before browser or document defaults',async()=>{
 const r=runtime(true,{error:'服务暂时不可用'},503,'zh-CN','ar-SA');await vi.waitFor(()=>expect(r.status()).toBe(messages['ar-SA'].download_failed));
});
it('includes download errors for every supported APK language',()=>{
 const languages=JSON.parse(readFileSync(resolve('../internal/platform/templates/apk-language-buttons.json'),'utf8'));
 for(const language of Object.keys(languages)){
  const key=Object.keys(messages).find(k=>k===language)||Object.keys(messages).find(k=>k.split('-')[0]===language.split('-')[0]);
  expect(key,language).toBeTruthy();for(const field of ['download_failed','invalid_url','network_error'])expect(messages[key!][field],`${language}.${field}`).toBeTruthy();
 }
});
