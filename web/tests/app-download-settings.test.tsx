import { render, screen, cleanup } from '@testing-library/react';
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
 expect((screen.getByRole('combobox') as HTMLSelectElement).value).toBe('direct');
 await user.selectOptions(screen.getByRole('combobox'),'info');
 await user.click(screen.getByRole('button',{name:'保存设置'}));
 expect(fetcher).toHaveBeenCalledWith('/api/apps/app1/download-settings',expect.objectContaining({method:'PATCH',body:JSON.stringify({download_mode:'info'})}));
 expect(close).toHaveBeenCalledOnce();client.clear();
});
it('keeps the dialog open and shows save errors',async()=>{
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({error:'保存失败，请重试'}),{status:500,headers:{'Content-Type':'application/json'}})));
 const close=vi.fn();const client=new QueryClient();
 render(<QueryClientProvider client={client}><AppDownloadSettings app={{id:'app1',name:'测试应用'} as App} close={close}/></QueryClientProvider>);
 await userEvent.setup().click(screen.getByRole('button',{name:'保存设置'}));
 await screen.findByText('保存失败，请重试');expect(close).not.toHaveBeenCalled();client.clear();
});
