import { cleanup, render, screen, within, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { beforeEach, afterEach, expect, it, vi } from 'vitest';
import { AdConfigurationsPage } from '../src/ad-configurations';
let client:QueryClient;
const binding={resource_kind:'apps',resource_id:'apk1',name:'Test APK',slug:'sample',mode:'quick',enabled:true,configured:true,facebook_enabled:true,tiktok_enabled:false,facebook_pixel:'123456789',tiktok_pixel:''};
const config={mode:'quick',disabled:false,facebook_enabled:true,tiktok_enabled:false,facebook_pixel:'123456789',tiktok_pixel:'',open_facebook_event:'ViewContent',register_facebook_event:'CompleteRegistration',download_facebook_event:'Lead',open_tiktok_event:'ViewContent',register_tiktok_event:'CompleteRegistration',download_tiktok_event:'ClickButton',allowed_origin:'',deep_link:'',allow_client_events:false};
function Location(){return <output aria-label="route">{useLocation().pathname}</output>}
beforeEach(()=>{Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});});
afterEach(()=>{cleanup();client?.clear();vi.unstubAllGlobals();});
function start(rows:any[]=[], deleteResponse?:()=>Promise<Response>){const requests:{path:string;method?:string;body:any}[]=[];vi.stubGlobal('fetch',vi.fn(async(path:string,init?:RequestInit)=>{const body=init?.body?JSON.parse(String(init.body)):undefined;requests.push({path,method:init?.method,body});let value:any={};if(init?.method==='DELETE'){if(deleteResponse)return deleteResponse();rows=rows.filter(row=>path!==`/api/ad-configurations/${row.resource_kind}/${row.resource_id}`);value={ok:true};}else if(path==='/api/ad-configurations'&&!init?.method)value={data:rows};else if(path==='/api/ad-app-options')value={data:[{...binding,configured:rows.length>0},{...binding,resource_kind:'pwas',resource_id:'pwa1',name:'Test PWA',configured:false}]};else if(path.split('?')[0]==='/api/apps/apk1/ads')value={config,facebook_token_set:true,tiktok_token_set:false,facebook_token:'fixture-visible-token',tiktok_token:''};return new Response(JSON.stringify(value),{headers:{'Content-Type':'application/json'}});}));client=new QueryClient({defaultOptions:{queries:{retry:false}}});render(<QueryClientProvider client={client}><MemoryRouter><AdConfigurationsPage/><Location/></MemoryRouter></QueryClientProvider>);return {requests,user:userEvent.setup()};}
it('starts with saved configurations only and creates a quick PWA binding',async()=>{const{requests,user}=start();await screen.findByText('还没有回传配置');expect(requests.some(r=>r.path==='/api/apps'||r.path==='/api/pwas')).toBe(false);await user.click(screen.getAllByRole('button',{name:'创建'})[0]);const dialog=screen.getByRole('dialog');expect(within(dialog).getByRole('tab',{name:'快速创建'}).getAttribute('aria-selected')).toBe('true');expect((within(dialog).getByLabelText('回传事件') as HTMLSelectElement).value).toBe('Download');expect(within(dialog).getByRole('option',{name:'sample · Test APK'})).toBeTruthy();expect(within(dialog).queryByText('APK 开始下载时回传，PWA 点击安装时回传。')).toBeNull();await user.selectOptions(within(dialog).getByLabelText('应用类型'),'pwas');await user.selectOptions(within(dialog).getByLabelText('关联应用'),'pwa1');await user.type(within(dialog).getByLabelText('Pixel ID'),'123456789');await user.type(within(dialog).getByLabelText('Access Token'),'fixture-token');expect(within(dialog).queryByLabelText(/App Link/)).toBeNull();await user.click(within(dialog).getByRole('button',{name:'保存配置'}));await waitFor(()=>expect(requests.find(r=>r.method==='POST')).toBeTruthy());expect(requests.find(r=>r.method==='POST')?.body).toMatchObject({resource_kind:'pwas',resource_id:'pwa1',config:{mode:'quick',download_facebook_event:'Download'},facebook_token:'fixture-token'});});
it('shows both platform Pixel IDs and reveals saved tokens only in the editor and preserves unchanged credentials',async()=>{const{requests,user}=start([{...binding,tiktok_enabled:true,tiktok_pixel:'TIKTOKPIXEL'}]);await screen.findByText('123456789');expect(screen.getByRole('columnheader',{name:'Pixel ID'})).toBeTruthy();expect(screen.getByText('TIKTOKPIXEL')).toBeTruthy();await user.click(screen.getByRole('button',{name:'编辑'}));const dialog=screen.getByRole('dialog');await within(dialog).findByDisplayValue('123456789');expect((within(dialog).getByLabelText('Access Token') as HTMLInputElement).value).toBe('fixture-visible-token');expect(within(dialog).getByLabelText('Access Token').getAttribute('type')).toBe('text');await user.click(within(dialog).getByRole('button',{name:'保存配置'}));await waitFor(()=>expect(requests.find(r=>r.method==='PUT')).toBeTruthy());expect(requests.find(r=>r.method==='PUT')?.body.facebook_token).toBe('');});
it('supports keyboard tabs and continues to the original deep configuration page',async()=>{const{requests,user}=start();await screen.findByText('还没有回传配置');await user.click(screen.getAllByRole('button',{name:'创建'})[0]);const dialog=screen.getByRole('dialog');const quick=within(dialog).getByRole('tab',{name:'快速创建'});quick.focus();await user.keyboard('{ArrowRight}');expect(within(dialog).getByRole('tab',{name:'深度创建'}).getAttribute('aria-selected')).toBe('true');await user.selectOptions(within(dialog).getByLabelText('关联应用'),'apk1');await user.click(within(dialog).getByRole('button',{name:'继续配置'}));expect(screen.getByLabelText('route').textContent).toBe('/apps/apk1/ads');expect(requests.some(r=>r.method==='POST')).toBe(false);});
it('disables a configuration through its owned resource without clearing channels',async()=>{const{requests,user}=start([binding]);await screen.findByText('123456789');await user.click(screen.getByRole('button',{name:'关闭'}));await waitFor(()=>expect(requests.find(r=>r.path.endsWith('/enabled'))).toBeTruthy());expect(requests.find(r=>r.path.endsWith('/enabled'))?.body).toEqual({enabled:false});});

it('requires confirmation and leaves the configuration untouched when cancelled',async()=>{
 const {requests,user}=start([binding]);await screen.findByText('123456789');
 await user.click(screen.getByRole('button',{name:'删除',exact:true}));
 const dialog=screen.getByRole('dialog',{name:'删除回传配置？'});
 expect(within(dialog).getByText('确认删除「Test APK」的回传配置？')).toBeTruthy();
 expect(within(dialog).getByText('删除后将停止该应用的广告回传，关联应用和历史事件日志会保留。')).toBeTruthy();
 expect(requests.some(r=>r.method==='DELETE')).toBe(false);
 await user.click(within(dialog).getByRole('button',{name:'取消'}));
 expect(screen.queryByRole('dialog')).toBeNull();
 expect(screen.getByText('123456789')).toBeTruthy();
 expect(requests.some(r=>r.method==='DELETE')).toBe(false);
});

it.each(['apps','pwas'])('deletes a %s configuration and makes its app selectable again',async kind=>{
 const row={...binding,resource_kind:kind,resource_id:kind==='apps'?'apk1':'pwa1'};
 const {requests,user}=start([row]);await screen.findByText('123456789');
 client.setQueryData(['ad-configuration-edit',kind,row.resource_id],{config});
 client.setQueryData(['ads',kind,row.resource_id],{api_secret:'cached-fixture-secret'});
 client.setQueryData(['ad-credentials',kind,row.resource_id],{facebook_token:'cached-fixture-token'});
 await user.click(screen.getByRole('button',{name:'删除',exact:true}));
 await user.click(within(screen.getByRole('dialog')).getByRole('button',{name:'删除配置'}));
 await screen.findByText('回传配置已删除');
 expect(requests.filter(r=>r.method==='DELETE').map(r=>r.path)).toEqual([`/api/ad-configurations/${kind}/${row.resource_id}`]);
 expect(requests.some(r=>r.path===`/api/${kind}/${row.resource_id}`&&r.method==='DELETE')).toBe(false);
 await screen.findByText('还没有回传配置');
 expect(client.getQueryData(['ad-configuration-edit',kind,row.resource_id])).toBeUndefined();
 expect(client.getQueryData(['ads',kind,row.resource_id])).toBeUndefined();
 expect(client.getQueryData(['ad-credentials',kind,row.resource_id])).toBeUndefined();
 await user.click(screen.getAllByRole('button',{name:'创建'})[0]);
 const dialog=screen.getByRole('dialog');
 await user.selectOptions(within(dialog).getByLabelText('应用类型'),kind);
 await waitFor(()=>expect((within(dialog).getByRole('option',{name:kind==='apps'?'sample · Test APK':'sample · Test PWA'}) as HTMLOptionElement).disabled).toBe(false));
});

it('keeps the confirmation open and preserves the row on a failed deletion',async()=>{
 const {requests,user}=start([binding],async()=>new Response(JSON.stringify({error:'删除失败，请重试'}),{status:500,headers:{'Content-Type':'application/json'}}));
 await screen.findByText('123456789');await user.click(screen.getByRole('button',{name:'删除',exact:true}));
 await user.click(within(screen.getByRole('dialog')).getByRole('button',{name:'删除配置'}));
 await within(screen.getByRole('dialog')).findByRole('alert');
 expect(screen.getByText('123456789')).toBeTruthy();
 expect(requests.filter(r=>r.method==='DELETE')).toHaveLength(1);
 expect(screen.queryByText('回传配置已删除')).toBeNull();
});

it('returns to the preceding page when deleting the final row on a page',async()=>{
 const rows=Array.from({length:21},(_,i)=>({...binding,resource_id:`apk${i}`,name:`APK ${i}`}));
 const {user}=start(rows);await screen.findByText('APK 0');
 await user.click(screen.getByRole('button',{name:'下一页'}));await screen.findByText('APK 20');
 await user.click(screen.getByRole('button',{name:'删除',exact:true}));
 await user.click(within(screen.getByRole('dialog')).getByRole('button',{name:'删除配置'}));
 await screen.findByText('回传配置已删除');await screen.findByText('APK 0');
 expect(screen.queryByText('APK 20')).toBeNull();
 expect(screen.getByText('1 / 1')).toBeTruthy();
});

it('changes only the reported name in quick mode and lists each platform’s own events',async()=>{
 const {requests,user}=start();await screen.findByText('还没有回传配置');await user.click(screen.getAllByRole('button',{name:'创建'})[0]);
 const dialog=screen.getByRole('dialog');await user.selectOptions(within(dialog).getByLabelText('关联应用'),'apk1');
 const meta=dialog.querySelectorAll('.ad-channel')[0] as HTMLElement;
 expect(within(meta).getByRole('option',{name:'注册成功 (CompleteRegistration)'})).toBeTruthy();
 await user.selectOptions(within(meta).getByLabelText('回传事件'),'Install');
 await user.type(within(meta).getByLabelText('Pixel ID'),'123456789');await user.type(within(meta).getByLabelText('Access Token'),'fixture-token');
 await user.click(within(dialog).getByLabelText('TikTok'));
 const tiktok=dialog.querySelectorAll('.ad-channel')[1] as HTMLElement;
 expect(within(tiktok).getByRole('option',{name:'ApplicationApproval'})).toBeTruthy();
 expect(within(meta).queryByRole('option',{name:'ApplicationApproval'})).toBeNull();
 expect((within(tiktok).getByLabelText('回传事件') as HTMLSelectElement).value).toBe('Download');
 await user.selectOptions(within(tiktok).getByLabelText('回传事件'),'@custom');
 await user.clear(within(tiktok).getByRole('textbox',{name:'自定义'}));await user.type(within(tiktok).getByRole('textbox',{name:'自定义'}),'CustomerInstallClick');
 await user.type(within(tiktok).getByLabelText('Pixel ID'),'TIKTOKPIXEL');await user.type(within(tiktok).getByLabelText('Access Token'),'fixture-token');
 await user.click(within(dialog).getByRole('button',{name:'保存配置'}));
 await waitFor(()=>expect(requests.find(r=>r.method==='POST')).toBeTruthy());
 expect(requests.find(r=>r.method==='POST')?.body.config).toMatchObject({mode:'quick',download_facebook_event:'Install',download_tiktok_event:'CustomerInstallClick',open_facebook_event:'ViewContent',register_facebook_event:'CompleteRegistration'});
});

 it('invalidates related application and editor data when advertising is toggled',async()=>{
  const {user}=start([binding]);await screen.findByText('123456789');
  const related=[['app','apk1'],['ads','apps','apk1'],['ad-app-options'],['ad-credentials','apps','apk1'],['cloak-targets']];
  for(const key of related)client.setQueryData(key,{old:true});
  await user.click(screen.getByRole('button',{name:'关闭'}));
  await waitFor(()=>{for(const key of related)expect(client.getQueryState(key)?.isInvalidated).toBe(true);});
 });
