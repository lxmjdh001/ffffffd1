import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppDownloadSettings } from '../src/app-download-settings';
import type { App } from '../src/types';

beforeEach(()=>{
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});
 Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});
});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();});
it('saves just the selected mode and closes after success',async()=>{
 const fetcher=vi.fn(async()=>new Response(JSON.stringify({download_mode:'info'}),{headers:{'Content-Type':'application/json'}}));vi.stubGlobal('fetch',fetcher);
 const close=vi.fn();const user=userEvent.setup();const client=new QueryClient();
 render(<QueryClientProvider client={client}><AppDownloadSettings app={{id:'app1',name:'测试应用'} as App} close={close}/></QueryClientProvider>);
 expect(screen.queryByRole('combobox')).toBeNull();
 expect(screen.getByRole('tab',{name:'直跳下载',selected:true})).toBeTruthy();
 expect(screen.getByRole('tab',{name:'跳转详情页',selected:false})).toBeTruthy();
 await user.click(screen.getByRole('tab',{name:'跳转详情页'}));
 expect(screen.getByRole('tab',{name:'跳转详情页',selected:true})).toBeTruthy();
 expect(screen.getByRole('tabpanel').textContent).toContain('先展示详情页');
 expect(fetcher).not.toHaveBeenCalled();
 await user.click(screen.getByRole('button',{name:'保存设置'}));
 expect(fetcher).toHaveBeenCalledWith('/api/apps/app1/download-settings',expect.objectContaining({method:'PATCH',body:JSON.stringify({download_mode:'info'})}));
 expect(close).toHaveBeenCalledOnce();client.clear();
});
it('loads the saved info mode and can save direct download without changing other app fields',async()=>{
 const updated={id:'app1',name:'测试应用',download_mode:'direct'} as App;
 const fetcher=vi.fn(async()=>new Response(JSON.stringify(updated)));vi.stubGlobal('fetch',fetcher);
 const close=vi.fn();const onSaved=vi.fn();const user=userEvent.setup();const client=new QueryClient();
 render(<QueryClientProvider client={client}><AppDownloadSettings app={{...updated,download_mode:'info'}} close={close} onSaved={onSaved}/></QueryClientProvider>);
 expect(screen.getByRole('tab',{name:'跳转详情页',selected:true})).toBeTruthy();
 await user.click(screen.getByRole('tab',{name:'直跳下载'}));
 expect(screen.getByRole('tabpanel').textContent).toContain('直接开始下载');
 await user.click(screen.getByRole('button',{name:'保存设置'}));
 expect(fetcher).toHaveBeenCalledWith('/api/apps/app1/download-settings',expect.objectContaining({method:'PATCH',body:JSON.stringify({download_mode:'direct'})}));
 expect(onSaved).toHaveBeenCalledWith(updated);expect(close).toHaveBeenCalledOnce();client.clear();
});
it('supports arrow keys, Home and End without submitting the form',async()=>{
 const fetcher=vi.fn();vi.stubGlobal('fetch',fetcher);
 const client=new QueryClient();const user=userEvent.setup();
 render(<QueryClientProvider client={client}><AppDownloadSettings app={{id:'app1',name:'测试应用'} as App} close={vi.fn()}/></QueryClientProvider>);
 screen.getByRole('tab',{name:'直跳下载'}).focus();
 await user.keyboard('{ArrowRight}');
 expect(document.activeElement).toBe(screen.getByRole('tab',{name:'跳转详情页',selected:true}));
 await user.keyboard('{ArrowRight}');
 expect(document.activeElement).toBe(screen.getByRole('tab',{name:'直跳下载',selected:true}));
 await user.keyboard('{ArrowLeft}');
 expect(document.activeElement).toBe(screen.getByRole('tab',{name:'跳转详情页',selected:true}));
 await user.keyboard('{Home}');expect(screen.getByRole('tab',{name:'直跳下载',selected:true})).toBe(document.activeElement);
 await user.keyboard('{End}{Enter}');expect(screen.getByRole('tab',{name:'跳转详情页',selected:true})).toBe(document.activeElement);
 expect(fetcher).not.toHaveBeenCalled();client.clear();
});
it('disables mode changes and closing while the save is pending',async()=>{
 let resolve!:(response:Response)=>void;
 vi.stubGlobal('fetch',vi.fn(()=>new Promise<Response>(done=>{resolve=done;})));
 const client=new QueryClient();const close=vi.fn();const user=userEvent.setup();
 render(<QueryClientProvider client={client}><AppDownloadSettings app={{id:'app1',name:'测试应用'} as App} close={close}/></QueryClientProvider>);
 await user.click(screen.getByRole('tab',{name:'跳转详情页'}));
 await user.click(screen.getByRole('button',{name:'保存设置'}));
 for(const tab of screen.getAllByRole('tab'))expect((tab as HTMLButtonElement).disabled).toBe(true);
 expect((screen.getByRole('button',{name:'取消'}) as HTMLButtonElement).disabled).toBe(true);
 await user.click(screen.getByRole('tab',{name:'直跳下载'}));
 await user.click(screen.getByRole('button',{name:'关闭'}));
 expect(close).not.toHaveBeenCalled();expect(screen.getByRole('tab',{name:'跳转详情页',selected:true})).toBeTruthy();
 resolve(new Response(JSON.stringify({id:'app1',download_mode:'info'})));
 await waitFor(()=>expect(close).toHaveBeenCalledOnce());client.clear();
});
it('cancels a changed choice without making a save request',async()=>{
 const fetcher=vi.fn();vi.stubGlobal('fetch',fetcher);
 const client=new QueryClient();const close=vi.fn();const user=userEvent.setup();
 render(<QueryClientProvider client={client}><AppDownloadSettings app={{id:'app1',name:'测试应用'} as App} close={close}/></QueryClientProvider>);
 await user.click(screen.getByRole('tab',{name:'跳转详情页'}));
 await user.click(screen.getByRole('button',{name:'取消'}));
 expect(close).toHaveBeenCalledOnce();expect(fetcher).not.toHaveBeenCalled();client.clear();
});
it('keeps the dialog open and shows save errors',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({error:'保存失败，请重试'}),{status:500,headers:{'Content-Type':'application/json'}})));
 const close=vi.fn();const client=new QueryClient();
 render(<QueryClientProvider client={client}><AppDownloadSettings app={{id:'app1',name:'测试应用'} as App} close={close}/></QueryClientProvider>);
 await userEvent.setup().click(screen.getByRole('button',{name:'保存设置'}));
 await screen.findByText('保存失败，请重试');expect(close).not.toHaveBeenCalled();client.clear();
});
