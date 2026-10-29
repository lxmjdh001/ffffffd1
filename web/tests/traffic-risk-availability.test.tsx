import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { TrafficPage } from '../src/traffic';

let client: QueryClient;
beforeEach(()=>{
  Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});
  Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});
});
afterEach(()=>{cleanup();client?.clear();vi.unstubAllGlobals();vi.restoreAllMocks();});
function open(status:string){
  const item={pool:{name:'分1',slug:'route',mode:'sequential',enabled:true,risk_enabled:true},entry_url:'https://route.example/r/route',eligible_link_ids:status==='unsafe'?['first']:['first','second'],fallback_link_id:status==='unsafe'?'first':'',links:[{id:'first',position:0,url:'https://a.example/a.apk',status,error:'',checked_at:null},{id:'second',position:1,url:'https://b.example/b.apk',status,error:'',checked_at:null}]};
  vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify({paid:true,configured:true,data:[item]}),{headers:{'Content-Type':'application/json'}})));
  client=new QueryClient({defaultOptions:{queries:{retry:false}}});
  render(<QueryClientProvider client={client}><MemoryRouter><TrafficPage/></MemoryRouter></QueryClientProvider>);
  return userEvent.setup();
}
it('shows one retained fallback without hiding either unsafe verdict',async()=>{
  const user=open('unsafe');await screen.findByText('分1');
  expect(screen.getByText('2 / 1')).toBeTruthy();
  await user.click(screen.getByRole('button',{name:'风险检测'}));
  const modal=screen.getByRole('heading',{name:'分1 · 风险检测'}).closest('dialog')!;
  expect(within(modal).getAllByText('命中风险')).toHaveLength(2);
  expect(within(modal).getAllByText('兜底保留')).toHaveLength(1);
  expect(within(modal).getByText('不参与')).toBeTruthy();
  expect(within(modal).getByRole('status').textContent).toBe('当前保留 1 条兜底链接；兜底不代表通过安全检测。');
});
it.each(['pending','unknown'])('keeps all %s links routable without claiming they passed',async(status)=>{
  const user=open(status);await screen.findByText('分1');expect(screen.getByText('2 / 2')).toBeTruthy();
  expect(screen.queryByText('兜底保留')).toBeNull();
  await user.click(screen.getByRole('button',{name:'风险检测'}));
  const modal=screen.getByRole('heading',{name:'分1 · 风险检测'}).closest('dialog')!;
  expect(within(modal).getAllByText(status==='pending'?'等待检测':'检测失败')).toHaveLength(2);
  expect(within(modal).getAllByText('参与',{exact:true})).toHaveLength(2);
  expect(within(modal).queryByText('未命中风险')).toBeNull();
  expect(within(modal).queryByText(/120/)).toBeNull();
});
