import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppCreatePage, AppDetailPage, AppsPage, UploadDialog } from '../src/apps';
import { ConfigContext } from '../src/ui';
import { upload } from '../src/api';
import type { App, Config } from '../src/types';
import type { DIYLandingReference } from '../src/diy-landing-types';

const { list, notify } = vi.hoisted(() => ({ list: vi.fn(), notify: vi.fn() }));
vi.mock('react-admin', () => ({ useGetList: list, useNotify: () => notify }));
vi.mock('../src/download-domain',()=>({useDownloadDomain:()=>({data:{download_base_url:''}})}));
vi.mock('../src/api', async () => ({ ...await vi.importActual('../src/api'), upload: vi.fn() }));
vi.mock('../src/landing-source-selector', () => ({ LandingSourceSelector: ({ value, onChange }: { value?: DIYLandingReference; onChange:(value:DIYLandingReference|undefined)=>void }) => <><label>测试DIY版本<select required data-field="diy_landing" value={value?.revision_id || ''} onChange={event=>onChange(event.target.value?{page_id:'page-one',revision_id:event.target.value}:undefined)}><option value="">请选择</option><option value="revision-one">活动页 v1</option><option value="revision-two">活动页 v2</option></select></label>{value&&<button type="button" onClick={()=>onChange({...value,content:{'text-0':'客户标题','primary-label':'下载体验','image-0':'data:image/png;base64,abc'}})}>设置测试页面内容</button>}</> }));
let client: QueryClient;
beforeEach(() => {
 client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
 list.mockReturnValue({ data: [], total: 0, isPending: false });
 Object.defineProperty(HTMLDialogElement.prototype, 'showModal', { configurable: true, value() { this.setAttribute('open', ''); } });
 Object.defineProperty(HTMLDialogElement.prototype, 'close', { configurable: true, value() { this.removeAttribute('open'); } });
});
afterEach(() => { cleanup(); client.clear(); vi.mocked(upload).mockReset(); vi.restoreAllMocks(); vi.unstubAllGlobals(); });

function setup(path = '/apps', replace?: App) {
 const close = vi.fn();
 render(<QueryClientProvider client={client}><ConfigContext.Provider value={{ config: { max_upload_bytes: 10 * 1024 * 1024, development: true } as Config, reload: vi.fn() }}><MemoryRouter initialEntries={[path]}><Routes>
  <Route path="/apps" element={<AppsPage/>}/><Route path="/apps/new" element={<AppCreatePage/>}/><Route path="/apps/:id/design" element={<h1>详情页设计</h1>}/><Route path="/apps/:id" element={<AppDetailPage/>}/>
  <Route path="/replace" element={<UploadDialog app={replace} close={close} done={vi.fn()}/>}/>
 </Routes></MemoryRouter></ConfigContext.Provider></QueryClientProvider>);
 return { user: userEvent.setup(), close };
}

it.each([false, true])('opens a full creation page whether the account already has an APK: %s', async exists => {
 if (exists) list.mockReturnValue({ data: [{ id: 'old', slug: 'old-app', name: 'Existing APK', platform: 'Android', size: 100, downloads: 0, enabled: true }], total: 1, isPending: false });
 const { user } = setup();
 const links = screen.getAllByRole('link', { name: '上传应用' });
 expect(links.every(link => link.getAttribute('href') === '/apps/new')).toBe(true);
 await user.click(links[0]);
 expect(screen.getByRole('heading', { name: '上传新应用' })).toBeTruthy();
 expect(screen.queryByRole('dialog')).toBeNull();
 expect(screen.getByLabelText('应用名称')).toBeTruthy();
 await user.click(screen.getByRole('link', { name: '返回应用列表' }));
 expect(screen.getByRole('heading', { name: '应用管理' })).toBeTruthy();
});

it('sends old upload links to the same creation page', () => {
 setup('/apps?upload=1');
 expect(screen.getByRole('heading', { name: '上传新应用' })).toBeTruthy();
 expect(screen.queryByRole('dialog')).toBeNull();
});

it('publishes from the page and continues directly to detail-page design', async () => {
 const { user } = setup('/apps/new');
 await user.type(screen.getByLabelText('应用名称'), 'Second APK');
 await user.type(screen.getByLabelText(/分享标识/), 'second-app');
 await user.upload(document.querySelector<HTMLInputElement>('input[type=file]')!, new File(['APK'], 'new.apk', { type: 'application/vnd.android.package-archive' }));
 vi.mocked(upload).mockResolvedValue({ id: 'new-app' } as App);
 await user.click(screen.getByRole('button', { name: '上传并发布' }));
 await screen.findByRole('heading', { name: '详情页设计' });
 expect(upload).toHaveBeenCalledOnce();
 const [path, body] = vi.mocked(upload).mock.calls[0];
 expect(path).toBe('/apps'); expect(body.get('name')).toBe('Second APK'); expect(body.get('slug')).toBe('second-app'); expect(body.get('landing_template')).toBe('google_play');
});

it('keeps package replacement in its existing dialog and blocks closing mid-upload', async () => {
 const { user, close } = setup('/replace', { id: 'existing', name: 'Existing APK', version: '1.0.0' } as App);
 const dialog = within(screen.getByRole('dialog'));
 expect(dialog.getByRole('heading', { name: '更新应用安装包' })).toBeTruthy();
 await user.upload(screen.getByRole('dialog').querySelector<HTMLInputElement>('input[type=file]')!, new File(['APK'], 'update.apk', { type: 'application/vnd.android.package-archive' }));
 vi.mocked(upload).mockImplementation((_path, _body, _progress, signal) => new Promise((_resolve, reject) => { signal?.addEventListener('abort', () => reject(new Error('Upload cancelled'))); }));
 await user.click(dialog.getByRole('button', { name: '更新版本' }));
 await waitFor(() => expect(upload).toHaveBeenCalledOnce());
 await user.click(dialog.getByRole('button', { name: '关闭' })); expect(close).not.toHaveBeenCalled();
 await user.click(dialog.getByRole('button', { name: '取消上传' }));
 await waitFor(() => expect(dialog.getByRole('button', { name: '更新版本' })).toBeTruthy());
 expect(vi.mocked(upload).mock.calls[0][0]).toBe('/apps/existing/file');
 expect(vi.mocked(upload).mock.calls[0][1].has('landing_source')).toBe(false);
});

it('creates an APK with a pinned DIY version and the details-page download mode', async () => {
 const {user}=setup('/apps/new');
 await user.type(screen.getByLabelText('应用名称'),'DIY APK');
 await user.click(screen.getByRole('button',{name:'DIY落地页'}));
 await user.selectOptions(screen.getByLabelText('测试DIY版本'),'revision-two');
 await user.upload(document.querySelector<HTMLInputElement>('input[type=file]')!,new File(['APK'],'diy.apk',{type:'application/vnd.android.package-archive'}));
 vi.mocked(upload).mockResolvedValue({id:'new-app'} as App);
 await user.click(screen.getByRole('button',{name:'上传并发布'}));
 await screen.findByRole('heading',{name:'详情页设计'});
 const body=vi.mocked(upload).mock.calls[0][1];
 expect(Object.fromEntries(['landing_source','diy_page_id','diy_revision_id','download_mode'].map(key=>[key,body.get(key)]))).toEqual({landing_source:'diy',diy_page_id:'page-one',diy_revision_id:'revision-two',download_mode:'info'});
});

it('includes application-specific page edits in APK creation multipart fields', async () => {
 const {user}=setup('/apps/new');
 await user.type(screen.getByLabelText('应用名称'),'Editable DIY APK');
 await user.click(screen.getByRole('button',{name:'DIY落地页'}));
 await user.selectOptions(screen.getByLabelText('测试DIY版本'),'revision-one');
 await user.click(screen.getByRole('button',{name:'设置测试页面内容'}));
 await user.upload(document.querySelector<HTMLInputElement>('input[type=file]')!,new File(['APK'],'editable.apk',{type:'application/vnd.android.package-archive'}));
 vi.mocked(upload).mockResolvedValue({id:'new-app'} as App);
 await user.click(screen.getByRole('button',{name:'上传并发布'}));
 await screen.findByRole('heading',{name:'详情页设计'});
 const body=vi.mocked(upload).mock.calls[0][1];
 expect(JSON.parse(String(body.get('diy_content')))).toEqual({'text-0':'客户标题','primary-label':'下载体验','image-0':'data:image/png;base64,abc'});
 expect(body.get('diy_page_id')).toBe('page-one');expect(body.get('diy_revision_id')).toBe('revision-one');
});

it('retains edited DIY content when the APK basic information is saved',async()=>{
 const content={'text-0':'APK 专属标题','primary-label':'立即体验'},writes:unknown[]=[];
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,options:RequestInit={})=>{
  const path=new URL(String(input),'https://console.example').pathname;
  if(options.method==='PATCH'){writes.push(JSON.parse(String(options.body)));return new Response('{}');}
  if(path==='/api/apps/existing')return new Response(JSON.stringify({id:'existing',slug:'existing',name:'DIY APK',version:'1',description:'',size:10,platform:'Android',sha256:'abc',enabled:true,downloads:0,landing_source:'diy',diy_landing:{page_id:'page-one',revision_id:'revision-one',content}}));
  return new Response(JSON.stringify({data:[],download_base_url:''}));
 }));
 const {user}=setup('/apps/existing');await screen.findByLabelText('测试DIY版本');
 await user.click(screen.getByRole('button',{name:'保存修改'}));await screen.findByRole('heading',{name:'应用管理'});
 expect(writes[0]).toMatchObject({diy_landing:{page_id:'page-one',revision_id:'revision-one',content}});
});

it('edits the APK DIY binding and explicitly clears it when switching to a system template',async()=>{
 const writes:unknown[]=[];
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,options:RequestInit={})=>{
  const path=new URL(String(input),'https://console.example').pathname;
  if(options.method==='PATCH'){writes.push(JSON.parse(String(options.body)));return new Response('{}');}
  if(path==='/api/apps/existing')return new Response(JSON.stringify({id:'existing',slug:'existing',name:'DIY APK',version:'1',description:'',size:10,platform:'Android',sha256:'abc',enabled:true,downloads:0,landing_source:'diy',diy_landing:{page_id:'page-one',revision_id:'revision-one'}}));
  return new Response(JSON.stringify({data:[],download_base_url:''}));
 }));
 const {user}=setup('/apps/existing');
 expect((await screen.findByLabelText('测试DIY版本') as HTMLSelectElement).value).toBe('revision-one');
 await user.click(screen.getByRole('button',{name:'Google Play 商店模板'}));
 await user.click(screen.getByRole('button',{name:'保存修改'}));
 await screen.findByRole('heading',{name:'应用管理'});
 expect(writes[0]).toMatchObject({landing_source:'system'});
 expect(writes[0]).not.toHaveProperty('diy_landing');
});
