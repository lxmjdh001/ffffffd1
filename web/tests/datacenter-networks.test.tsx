import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { DatacenterNetworksPage } from '../src/datacenter-networks';
const permission=vi.hoisted(()=>({role:'admin'}));
vi.mock('react-admin',async()=>({...await vi.importActual('react-admin'),usePermissions:()=>({permissions:permission.role,isPending:false})}));
let client:QueryClient;let rows:any[];let mutations:{method:string;body:any}[];
const reply=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status});
beforeEach(()=>{
 permission.role='admin';rows=[{id:'rule1',cidr:'2401:b60::/32',remark:'初始名单',enabled:true,updated_at:'2026-11-07T00:00:00Z'}];mutations=[];client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(){this.setAttribute('open','');}});Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(){this.removeAttribute('open');}});
 vi.stubGlobal('fetch',vi.fn(async(input:string,options?:RequestInit)=>{const u=new URL(String(input),'http://localhost');const method=options?.method||'GET';const body=options?.body?JSON.parse(String(options.body)):undefined;
 if(method==='GET'){const q=u.searchParams.get('q')||'';const data=rows.filter(r=>(r.cidr+r.remark).includes(q));return reply({data,total:data.length,rules_total:rows.length,enabled_total:rows.filter(r=>r.enabled).length});}
 mutations.push({method,body});
 if(u.pathname.endsWith('/import'))return body.entries.includes('invalid')?reply({error:'第 2 行：请输入有效的 IPv4、IPv6 或 CIDR 网段'},400):reply({inserted:1,duplicates:1});
 if(method==='POST'){const row={id:'new-rule',...body,updated_at:'2026-11-07T00:00:00Z'};rows.push(row);return reply(row);}
 if(method==='DELETE'){rows=rows.filter(r=>!u.pathname.endsWith('/'+r.id));return reply({ok:true});}
 rows=rows.map(r=>u.pathname.endsWith('/'+r.id)?{...r,...body}:r);return reply(rows[0]);
 }));
});
afterEach(()=>{cleanup();client.clear();vi.unstubAllGlobals();});
const open=()=>render(<QueryClientProvider client={client}><DatacenterNetworksPage/></QueryClientProvider>);
it('creates, edits, disables, searches and deletes IP rules',async()=>{
 const user=userEvent.setup();open();await screen.findByText('2401:b60::/32');
 await user.click(screen.getByRole('button',{name:'新增规则'}));let dialog=within(screen.getByRole('dialog'));await user.type(dialog.getByLabelText('IP / CIDR'),'203.0.113.0/24');await user.type(dialog.getByLabelText('备注'),'测试网段');await user.click(dialog.getByRole('button',{name:'保存',exact:true}));await screen.findByText('203.0.113.0/24');
 await user.click(within(screen.getByText('2401:b60::/32').closest('tr')!).getByRole('button',{name:'编辑'}));dialog=within(screen.getByRole('dialog'));await user.clear(dialog.getByLabelText('备注'));await user.type(dialog.getByLabelText('备注'),'updated');await user.click(dialog.getByRole('button',{name:'保存',exact:true}));await screen.findByText('updated');
 await user.click(screen.getByRole('button',{name:'停用 2401:b60::/32'}));await screen.findByRole('button',{name:'启用 2401:b60::/32'});expect(mutations.some(m=>m.method==='PATCH'&&m.body.enabled===false)).toBe(true);
 await user.type(screen.getByRole('textbox',{name:'搜索 IP、CIDR 或备注'}),'203.0.113');await waitFor(()=>expect(screen.queryByText('2401:b60::/32')).toBeNull());await user.clear(screen.getByRole('textbox',{name:'搜索 IP、CIDR 或备注'}));await screen.findByText('2401:b60::/32');
 await user.click(within(screen.getByText('2401:b60::/32').closest('tr')!).getByRole('button',{name:'删除'}));await user.click(within(screen.getByRole('dialog')).getByRole('button',{name:'确认删除'}));await waitFor(()=>expect(screen.queryByText('2401:b60::/32')).toBeNull());
});
it('keeps invalid imports visible and reports skipped duplicates',async()=>{
 const user=userEvent.setup();open();await screen.findByText('2401:b60::/32');await user.click(screen.getByRole('button',{name:'批量导入'}));const dialog=within(screen.getByRole('dialog'));const input=dialog.getByLabelText('IP / CIDR 名单');await user.type(input,'192.0.2.1\ninvalid');await user.click(dialog.getByRole('button',{name:'导入',exact:true}));await screen.findByText('第 2 行：请输入有效的 IPv4、IPv6 或 CIDR 网段');expect((input as HTMLTextAreaElement).value).toContain('invalid');await user.clear(input);await user.type(input,'192.0.2.1\n192.0.2.1');await user.click(dialog.getByRole('button',{name:'导入',exact:true}));await screen.findByText('已导入 1 条，跳过 1 条重复规则');
});
it('never fetches administrator rules for members',()=>{permission.role='member';open();expect(screen.getByText('此页面需要管理员权限')).toBeTruthy();expect(fetch).not.toHaveBeenCalled();});
