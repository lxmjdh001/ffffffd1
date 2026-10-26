import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AdminLandingTemplateLibraryPage, LandingTemplateLibraryPage, type LandingLibraryTemplate } from '../src/landing-template-library';
import { defaultDIYLandingOptions } from '../src/diy-landing-types';
import { setLocale } from '../src/i18n';

const access=vi.hoisted(()=>({role:'member'}));
vi.mock('react-admin',()=>({usePermissions:()=>({permissions:access.role,isPending:false}),useNotify:()=>vi.fn()}));
const descriptors={showModal:Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype,'showModal'),close:Object.getOwnPropertyDescriptor(HTMLDialogElement.prototype,'close')};
const response=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}});
const item=(values:Partial<LandingLibraryTemplate>={}):LandingLibraryTemplate=>({id:'template-one',name:'通用应用安装页',description:'适合应用推广的简洁页面',config:defaultDIYLandingOptions(),version:1,enabled:true,favorite:false,cover_status:'ready',cover_url:'/api/landing-template-covers/template-one.png',...values});
beforeEach(()=>{
 access.role='member';setLocale('zh-CN',false);
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value(this:HTMLDialogElement){this.setAttribute('open','');}});
 Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value(this:HTMLDialogElement){this.removeAttribute('open');}});
});
afterEach(()=>{cleanup();vi.useRealTimers();vi.unstubAllGlobals();vi.restoreAllMocks();for(const [name,descriptor] of Object.entries(descriptors)){if(descriptor)Object.defineProperty(HTMLDialogElement.prototype,name,descriptor);else delete (HTMLDialogElement.prototype as unknown as Record<string,unknown>)[name];}});
const library=()=>render(<MemoryRouter><LandingTemplateLibraryPage/></MemoryRouter>);

it('favorites and unfavorites a library template through the dedicated endpoints',async()=>{
 let favorite=false;const calls:{path:string;method:string}[]=[];
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  const path=String(input),method=options?.method||'GET';calls.push({path,method});
  if(path.endsWith('/favorite')){favorite=method==='POST';return response(method==='POST'?{favorite:true,reference:{page_id:'saved-page',revision_id:'saved-revision'}}:{ok:true});}
  return response({data:[item({favorite})],total:1});
 }));
 library();const user=userEvent.setup();await screen.findByRole('heading',{name:'通用应用安装页',level:2});
 expect(screen.getByRole('link',{name:'打开DIY落地页'}).getAttribute('target')).toBe('_blank');
 await user.click(screen.getByRole('button',{name:'收藏模版',exact:true}));
 await screen.findByText('已收藏，可在APK和PWA中选择使用。');
 expect(calls.some(call=>call.path==='/api/landing-templates/template-one/favorite'&&call.method==='POST')).toBe(true);
 await user.click(screen.getByRole('button',{name:'取消收藏',exact:true}));
 await screen.findByText('已取消收藏，已绑定应用继续使用原版本。');
 expect(calls.some(call=>call.path==='/api/landing-templates/template-one/favorite'&&call.method==='DELETE')).toBe(true);
});

it('filters the catalogue by favorites and supported app kind',async()=>{
 const paths:string[]=[];vi.stubGlobal('fetch',vi.fn(async(input:string)=>{paths.push(String(input));return response({data:[],total:0});}));
 library();const user=userEvent.setup();await screen.findByText('模版库正在准备中');
 await user.click(screen.getByRole('button',{name:'我的收藏',exact:true}));
 await user.selectOptions(screen.getByRole('combobox',{name:'适用应用'}),'pwa');
 await screen.findByText('还没有收藏的模版');
 await waitFor(()=>expect(paths.some(path=>path.includes('target=pwa')&&path.startsWith('/api/landing-favorites?'))).toBe(true));
});

it('loads source only when preview is opened and uses the isolated preview service',async()=>{
 const calls:string[]=[];vi.stubGlobal('fetch',vi.fn(async(input:string)=>{
  const path=String(input);calls.push(path);
  if(path.endsWith('/preview'))return response({preview_url:'/api/diy-landing-preview/library-preview',channel:'library-channel',issues:[]});
  if(path==='/api/landing-templates/template-one')return response(item({html:'<button data-d1-action="primary">Install</button>'}));
  return response({data:[item()],total:1});
 }));
 library();const user=userEvent.setup();await screen.findByRole('heading',{name:'通用应用安装页',level:2});
 expect(calls.some(path=>path.endsWith('/template-one'))).toBe(false);
 await user.click(screen.getByRole('button',{name:'预览模版 通用应用安装页'}));
 const frame=await screen.findByTitle('DIY落地页实时预览');
 expect(frame.getAttribute('src')).toBe('/api/diy-landing-preview/library-preview');
 expect(frame.getAttribute('sandbox')).toBe('allow-scripts');
 expect(calls.some(path=>/^\/api\/(?:apps|pwas|a|p)\//.test(path))).toBe(false);
});

it('hides administrative editing and never requests admin data for a member',()=>{
 const fetcher=vi.fn();vi.stubGlobal('fetch',fetcher);render(<AdminLandingTemplateLibraryPage/>);
 expect(screen.getByText('仅管理员可以管理模版库')).toBeTruthy();expect(fetcher).not.toHaveBeenCalled();
});

it('lets an administrator add HTML and publish it to the catalogue',async()=>{
 access.role='admin';let saved:Record<string,unknown>|undefined;const html='<button data-d1-action="primary">Install</button>';
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  const path=String(input);if(path.endsWith('/preview'))return response({preview_url:'/api/diy-landing-preview/admin-preview',issues:[]});
  if(options?.method==='POST'){saved=JSON.parse(String(options.body));return response(item({html}),201);}
  return response({data:saved?[item()]:[],total:saved?1:0});
 }));
 render(<AdminLandingTemplateLibraryPage/>);const user=userEvent.setup();await screen.findByText('还没有模版');
 await user.click(screen.getAllByRole('button',{name:'添加模版',exact:true})[0]);
 expect(screen.queryByRole('textbox',{name:'封面图片（选填）'})).toBeNull();
 expect(screen.getByText('保存后自动生成落地页真实封面，无需上传图片。')).toBeTruthy();
 await user.type(screen.getByRole('textbox',{name:'模版名称',exact:true}),'通用应用安装页');
 await user.type(screen.getByRole('textbox',{name:'模版说明',exact:true}),'适合应用推广的简洁页面');
 fireEvent.change(screen.getByRole('textbox',{name:'模版HTML代码',exact:true}),{target:{value:html}});
 await user.click(screen.getByRole('button',{name:'保存并上架',exact:true}));
 await screen.findByText('模版已保存，预览封面会自动更新。');
 expect(saved?.name).toBe('通用应用安装页');expect(saved?.html).toBe(html);expect(saved?.enabled).toBe(true);
 expect(saved?.config).toEqual(defaultDIYLandingOptions());expect(saved).not.toHaveProperty('cover_url');
});

it('previews a saved snapshot when its original template has been unpublished',async()=>{
 const paths:string[]=[];let previewBody:Record<string,unknown>|undefined;const saved=item({favorite:true,source_available:false,favorite_reference:{page_id:'saved-page',revision_id:'saved-v1'}});
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  const path=String(input);paths.push(path);
  if(path.endsWith('/preview')){previewBody=JSON.parse(String(options?.body));return response({preview_url:'/api/diy-landing-preview/saved-v1',issues:[]});}
  return response({data:[saved],total:1});
 }));
 library();const user=userEvent.setup();await screen.findByRole('heading',{name:saved.name,level:2});
 await user.click(screen.getByRole('button',{name:'我的收藏',exact:true}));
 await waitFor(()=>expect(paths.some(path=>path.startsWith('/api/landing-favorites?'))).toBe(true));
 await user.click(screen.getByRole('button',{name:'预览模版 通用应用安装页'}));
 const frame=await screen.findByTitle('DIY落地页实时预览');expect(frame.getAttribute('src')).toBe('/api/diy-landing-preview/saved-v1');
 expect(previewBody).toEqual({reference:saved.favorite_reference,mode:'apk'});
 expect(paths).not.toContain('/api/diy-landings/saved-page/revisions/saved-v1');
 expect(paths).not.toContain('/api/landing-templates/template-one');
});

it('includes the current version when an administrator unpublishes a template',async()=>{
 access.role='admin';let payload:Record<string,unknown>|undefined;
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  if(options?.method==='PUT'){payload=JSON.parse(String(options.body));return response(item({enabled:false,version:5}));}
  if(String(input)==='/api/admin/landing-templates/template-one')return response(item({html:'<h1>Version four</h1>',version:4}));
  return response({data:[item({enabled:!payload,version:payload?5:4})],total:1});
 }));
 render(<AdminLandingTemplateLibraryPage/>);const user=userEvent.setup();await screen.findByText('通用应用安装页');
 await user.click(screen.getByRole('button',{name:'下架',exact:true}));
 await screen.findByText('模版已下架，已收藏的版本继续有效。');
 expect(payload?.expected_version).toBe(4);expect(payload?.enabled).toBe(false);expect(payload?.html).toBe('<h1>Version four</h1>');
});


it('renders the actual generated cover and never substitutes a schematic page',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>response({data:[item()],total:1})));
 const view=library();await screen.findByRole('heading',{name:'通用应用安装页',level:2});
 const cover=view.container.querySelector('.landing-library-artwork>img');
 expect(cover?.getAttribute('src')).toBe('/api/landing-template-covers/template-one.png');
 expect(view.container.querySelector('[class*="mini-page"], [class*="mini-browser"], .landing-library-placeholder')).toBeNull();
 fireEvent.error(cover!);
 expect(screen.getByText('封面加载失败')).toBeTruthy();
 expect(view.container.querySelector('.landing-library-artwork>img')).toBeNull();
 expect(screen.getByText('点击预览查看落地页真实效果')).toBeTruthy();
});

it('shows a generation state without loading an old or illustrative cover',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>response({data:[item({cover_status:'processing',cover_url:'/old-cover.png'})],total:1})));
 const view=library();await screen.findByText('封面生成中');
 expect(screen.getByText('正在生成落地页的真实预览')).toBeTruthy();
 expect(view.container.querySelector('.landing-library-artwork>img')).toBeNull();
 expect(view.container.querySelector('.landing-library-artwork')?.getAttribute('aria-busy')).toBe('true');
});

it('keeps the real isolated preview available after cover generation fails',async()=>{
 const failed=item({cover_status:'failed',cover_url:undefined});
 vi.stubGlobal('fetch',vi.fn(async(input:string)=>{
  const path=String(input);
  if(path.endsWith('/preview'))return response({preview_url:'/api/diy-landing-preview/failed-cover',issues:[]});
  if(path==='/api/landing-templates/template-one')return response({...failed,html:'<h1>Real template</h1>'});
  return response({data:[failed],total:1});
 }));
 library();const user=userEvent.setup();await screen.findByText('封面生成失败');
 await user.click(screen.getByRole('button',{name:'预览模版 通用应用安装页'}));
 const frame=await screen.findByTitle('DIY落地页实时预览');
 expect(frame.getAttribute('src')).toBe('/api/diy-landing-preview/failed-cover');
});

it('quietly polls pending covers, pauses in hidden tabs, and stops once ready',async()=>{
 vi.useFakeTimers();let visible=true,ready=false,requests=0;
 vi.spyOn(document,'visibilityState','get').mockImplementation(()=>visible?'visible':'hidden');
 vi.stubGlobal('fetch',vi.fn(async()=>{
  requests++;return response({data:[item({cover_status:ready?'ready':'pending',cover_url:ready?'/api/landing-template-covers/generated.png':undefined})],total:1});
 }));
 const view=library();await act(async()=>{});
 expect(screen.getByText('封面生成中')).toBeTruthy();expect(requests).toBe(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000);});
 expect(requests).toBe(2);expect(screen.getByRole('heading',{name:'通用应用安装页',level:2})).toBeTruthy();
 visible=false;fireEvent(document,new Event('visibilitychange'));
 await act(async()=>{await vi.advanceTimersByTimeAsync(9000);});
 expect(requests).toBe(2);
 ready=true;visible=true;await act(async()=>{fireEvent(document,new Event('visibilitychange'));});
 expect(requests).toBe(3);expect(view.container.querySelector('.landing-library-artwork>img')?.getAttribute('src')).toBe('/api/landing-template-covers/generated.png');
 await act(async()=>{await vi.advanceTimersByTimeAsync(9000);});
 expect(requests).toBe(3);
});

it('aborts an in-flight cover refresh and removes timers on unmount',async()=>{
 vi.useFakeTimers();let requests=0,signal:AbortSignal|undefined;
 vi.stubGlobal('fetch',vi.fn(async(_input:string,options?:RequestInit)=>{
  requests++;
  if(requests===1)return response({data:[item({cover_status:'pending',cover_url:undefined})],total:1});
  signal=options?.signal as AbortSignal;
  return new Promise<Response>(()=>{});
 }));
 const view=library();await act(async()=>{});
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000);});
 expect(requests).toBe(2);expect(signal?.aborted).toBe(false);
 view.unmount();expect(signal?.aborted).toBe(true);
 await act(async()=>{await vi.advanceTimersByTimeAsync(9000);});
 expect(requests).toBe(2);
});

it('lets an administrator retry a failed cover without resaving template content',async()=>{
 access.role='admin';let retried=false;const calls:{path:string;method:string;body?:string}[]=[];
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  const path=String(input),method=options?.method||'GET';calls.push({path,method,body:options?.body as string|undefined});
  if(path.endsWith('/cover/retry')){retried=true;return response({cover_status:'pending'});}
  return response({data:[item({cover_status:retried?'pending':'failed',cover_error:retried?'':'截图服务暂不可用',cover_url:undefined})],total:1});
 }));
 render(<AdminLandingTemplateLibraryPage/>);const user=userEvent.setup();await screen.findByText('封面生成失败');
 await user.click(screen.getByRole('button',{name:'重新生成封面',exact:true}));
 await screen.findByText('已开始重新生成预览封面。');
 expect(screen.getByText('封面生成中')).toBeTruthy();
 expect(calls.find(call=>call.method==='POST')).toEqual({path:'/api/admin/landing-templates/template-one/cover/retry',method:'POST',body:'{}'});
 expect(calls.some(call=>call.method==='PUT')).toBe(false);
});

it('refreshes administrator cover status until generation is complete',async()=>{
 access.role='admin';vi.useFakeTimers();let requests=0;
 vi.stubGlobal('fetch',vi.fn(async()=>{
  requests++;return response({data:[item({cover_status:requests===1?'processing':'ready'})],total:1});
 }));
 render(<AdminLandingTemplateLibraryPage/>);await act(async()=>{});
 expect(screen.getByText('封面生成中')).toBeTruthy();
 await act(async()=>{await vi.advanceTimersByTimeAsync(3000);});
 expect(screen.getByText('封面已生成')).toBeTruthy();expect(requests).toBe(2);
 await act(async()=>{await vi.advanceTimersByTimeAsync(9000);});
 expect(requests).toBe(2);
});


it('shows the favorite screenshot version returned for the saved snapshot',async()=>{
 const savedCover='/api/landing-template-covers/saved-version-two.png';
 vi.stubGlobal('fetch',vi.fn(async(input:string)=>{
  const saved=String(input).startsWith('/api/landing-favorites?');
  return response({data:[item(saved?{version:2,favorite:true,favorite_version:2,cover_url:savedCover,favorite_reference:{page_id:'saved-page',revision_id:'saved-v2'}}:{version:5,cover_url:'/api/landing-template-covers/source-version-five.png'})],total:1});
 }));
 const view=library();const user=userEvent.setup();await screen.findByRole('heading',{name:'通用应用安装页',level:2});
 expect(view.container.querySelector('.landing-library-artwork>img')?.getAttribute('src')).toBe('/api/landing-template-covers/source-version-five.png');
 await user.click(screen.getByRole('button',{name:'我的收藏',exact:true}));
 await waitFor(()=>expect(view.container.querySelector('.landing-library-artwork>img')?.getAttribute('src')).toBe(savedCover));
 expect(screen.getByText('v2')).toBeTruthy();
});
