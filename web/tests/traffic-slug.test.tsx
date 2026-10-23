import { cleanup, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach,beforeEach,expect,it,vi } from 'vitest';
import { TrafficPage } from '../src/traffic';

beforeEach(()=>{
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});
 Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});
});
afterEach(()=>{cleanup();vi.unstubAllGlobals();});
it('sends a renamed identifier to the original pool endpoint',async()=>{
 const original='d99b86a938d25563aa3ccd10f1d12a88';let request:unknown;
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{
  if(options?.method==='PUT'){request={path:input,body:JSON.parse(String(options.body))};}
  return new Response(JSON.stringify({paid:false,configured:false,data:[{pool:{name:'应用分流',mode:'random',enabled:true,risk_enabled:false,slug:original},links:[{id:'link1',url:'https://one.example.com/app.apk',position:0,status:'pending'}],entry_url:'http://localhost:8088/r/'+original}]}),{headers:{'Content-Type':'application/json'}});
 }));
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});const user=userEvent.setup();
 render(<QueryClientProvider client={client}><MemoryRouter><TrafficPage/></MemoryRouter></QueryClientProvider>);
 await user.click(await screen.findByRole('button',{name:'编辑'}));
 const field=screen.getByRole('textbox',{name:/分流标识/});await user.clear(field);await user.type(field,'My-Route');
 expect(screen.getByText(/原 \/r\/d99b86/)).toBeTruthy();
 await user.click(screen.getByRole('button',{name:'保存分流'}));
 await waitFor(()=>expect(request).toBeDefined());
 expect(request).toMatchObject({path:'/api/traffic-pools/'+original,body:{slug:'my-route',enabled:true}});
 client.clear();
});
