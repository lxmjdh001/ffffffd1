import { render, screen, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { AppShareDrawer } from '../src/app-share-drawer';
import type { App } from '../src/types';

beforeEach(()=>{
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});
 Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});
});
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();});
it('lists permitted domains in a right drawer, saves a note, and copies the chosen URL',async()=>{
 let remark='主域名';let savedBody:unknown;
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  if(options?.method==='PATCH'){savedBody=JSON.parse(String(options.body));remark=(savedBody as {remark:string}).remark;return new Response(JSON.stringify({remark}),{headers:{'Content-Type':'application/json'}});}
  return new Response(JSON.stringify({data:[{url:'https://one.example.com',remark,source:'platform',audience:'all'},{url:'https://two.example.com',remark:'备用域名',source:'custom',audience:'owner'}]}),{headers:{'Content-Type':'application/json'}});
 }));
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});const user=userEvent.setup();const clipboard=vi.spyOn(navigator.clipboard,'writeText').mockResolvedValue();
 render(<QueryClientProvider client={client}><AppShareDrawer app={{name:'测试应用',slug:'my-brand'} as App} close={vi.fn()}/></QueryClientProvider>);
 await screen.findByText('https://two.example.com');expect(document.querySelector('dialog')?.className).toContain('side-drawer');
 await user.click(screen.getAllByRole('button',{name:'复制'})[1]);expect(clipboard).toHaveBeenCalledWith('https://two.example.com/a/my-brand');
 expect(screen.queryByRole('textbox',{name:/域名备注/})).toBeNull();
 await user.click(screen.getByRole('button',{name:'备注 https://one.example.com'}));
 const note=screen.getByRole('textbox',{name:/域名备注/});await user.clear(note);await user.type(note,'投放 A');
 await user.click(screen.getByRole('button',{name:'保存备注'}));
 await waitFor(()=>expect(screen.queryByRole('textbox',{name:/域名备注/})).toBeNull());
 const display=await screen.findByText('投放 A');expect(display.className).toBe('share-domain-remark');
 expect(savedBody).toEqual({url:'https://one.example.com',remark:'投放 A'});client.clear();
});

it('opens download settings without leaving the drawer and keeps the saved mode',async()=>{
 const app={id:'app1',name:'测试应用',slug:'my-brand',download_mode:'direct'} as App;
 let savedMode='direct';
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  if(String(input).endsWith('/download-settings')){savedMode=JSON.parse(String(options?.body)).download_mode;return new Response(JSON.stringify({...app,download_mode:savedMode}),{headers:{'Content-Type':'application/json'}});}
  return new Response(JSON.stringify({data:[{url:'https://one.example.com',remark:'主域名',source:'platform',audience:'all'}]}),{headers:{'Content-Type':'application/json'}});
 }));
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});const user=userEvent.setup();
 render(<QueryClientProvider client={client}><AppShareDrawer app={app} close={vi.fn()}/></QueryClientProvider>);
 await screen.findByText('https://one.example.com');
 expect(screen.queryByText(/仅展示当前账号可用/)).toBeNull();
 expect(screen.queryByText(/分享链接将直接下载/)).toBeNull();
 await user.click(screen.getByRole('button',{name:'设置'}));
 await user.click(screen.getByRole('tab',{name:'跳转详情页'}));
 await user.click(screen.getByRole('button',{name:'保存设置'}));
 await waitFor(()=>expect(screen.queryByRole('heading',{name:'应用设置'})).toBeNull());
 expect(savedMode).toBe('info');expect(screen.getByRole('heading',{name:'选择域名复制链接'})).toBeTruthy();
 await user.click(screen.getByRole('button',{name:'设置'}));
 expect(screen.getByRole('tab',{name:'跳转详情页',selected:true})).toBeTruthy();
 client.clear();
});
