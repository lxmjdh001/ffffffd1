import {cleanup,render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {AdminListPage} from '../src/admin';
import {ImpersonationBanner} from '../src/impersonation';
import {authProvider,getMe} from '../src/api';
import type {ImpersonationInfo,User} from '../src/types';
const navigation=vi.hoisted(()=>vi.fn());
vi.mock('../src/account-context',()=>({reloadAccount:navigation}));
vi.mock('react-admin',()=>({usePermissions:()=>({permissions:'admin',isPending:false}),useNotify:()=>vi.fn()}));
const scope:ImpersonationInfo={administrator_id:'admin',administrator_name:'Admin',member_id:'member',member_name:'测试会员',expires_at:'2026-11-03T08:00:00Z',available:true};
const member={id:'member',name:'测试会员',email:'member@example.test',role:'member',disabled:false,balance_micros:1000000,held_micros:0,pro_until:null,created_at:'2026-10-31T00:00:00Z'} as User;
let client:QueryClient,writes:{path:string;headers:Headers}[],fail:boolean,gate:Promise<void>|undefined;
const json=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}});
beforeEach(()=>{
 client=new QueryClient({defaultOptions:{queries:{retry:false}}});writes=[];fail=false;gate=undefined;navigation.mockReset();
 vi.stubGlobal('fetch',vi.fn(async(input:RequestInfo|URL,init:RequestInit={})=>{
  const path=String(input).split('?')[0];
  if(init.method==='POST'){
   writes.push({path,headers:new Headers(init.headers)});if(gate)await gate;
   if(fail)return json({error:'代登录操作失败'},503);
   return json({user:path.includes('/stop')?{id:'admin',name:'Admin',role:'admin'}:member,csrf_token:'new-csrf'});
  }
  if(path==='/api/admin/users')return json({data:[member,{...member,id:'admin',name:'Admin',role:'admin'},{...member,id:'disabled',name:'停用会员',disabled:true}],total:3});
  if(path==='/api/me')return json({user:{id:'admin',name:'Admin',role:'admin'},csrf_token:'admin-csrf'});
  throw new Error(`Unexpected request ${path}`);
 }));
});
afterEach(()=>{cleanup();client.clear();vi.unstubAllGlobals();vi.restoreAllMocks();});
function open(node:React.ReactNode){render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);return userEvent.setup();}
it('enters a member from its name, clears account data and navigates only after the authorized request',async()=>{
 await getMe();client.setQueryData(['apps'],{private:'old-account'});const user=open(<AdminListPage resource="users"/>);
 await user.click(await screen.findByRole('button',{name:'进入 测试会员 的后台'}));
 await waitFor(()=>expect(navigation).toHaveBeenCalledWith('/'));
 expect(writes).toHaveLength(1);expect(writes[0].path).toBe('/api/admin/users/member/impersonation');expect(writes[0].headers.get('X-CSRF-Token')).toBe('admin-csrf');
 expect(client.getQueryData(['apps'])).toBeUndefined();expect(screen.queryByRole('button',{name:'进入 Admin 的后台'})).toBeNull();expect(screen.queryByRole('button',{name:'进入 停用会员 的后台'})).toBeNull();
});
it('retains the administrator screen and cached data if entering fails',async()=>{
 fail=true;client.setQueryData(['apps'],{private:'admin-account'});const user=open(<AdminListPage resource="users"/>);
 await user.click(await screen.findByRole('button',{name:'进入 测试会员 的后台'}));
 await screen.findByText('代登录操作失败');expect(navigation).not.toHaveBeenCalled();expect(client.getQueryData(['apps'])).toEqual({private:'admin-account'});
 expect((screen.getByRole('button',{name:'进入 测试会员 的后台'}) as HTMLButtonElement).disabled).toBe(false);
});
it('prevents duplicate entry requests while changing the account context',async()=>{
 let release!:()=>void;gate=new Promise<void>(resolve=>{release=resolve;});const user=open(<AdminListPage resource="users"/>);
 await user.dblClick(await screen.findByRole('button',{name:'进入 测试会员 的后台'}));expect(writes).toHaveLength(1);expect(navigation).not.toHaveBeenCalled();release();await waitFor(()=>expect(navigation).toHaveBeenCalledOnce());
});
it.each([true,false])('keeps a return control for an available=%s support scope and clears member data on return',async available=>{
 await getMe();client.setQueryData(['apps'],{private:'member-account'});const user=open(<ImpersonationBanner scope={{...scope,available}}/>);
 expect(screen.getByText('正在以 测试会员 的身份操作')).toBeTruthy();if(!available)expect(screen.getByText('代登录已到期或会员不可用，请返回管理员')).toBeTruthy();
 await user.click(screen.getByRole('button',{name:'返回管理员'}));await waitFor(()=>expect(navigation).toHaveBeenCalledWith('/admin/users'));expect(writes[0].path).toBe('/api/auth/impersonation/stop');expect(client.getQueryData(['apps'])).toBeUndefined();
});
it('preserves the return control and member context if restoration fails',async()=>{
 fail=true;client.setQueryData(['apps'],{private:'member-account'});const user=open(<ImpersonationBanner scope={scope}/>);await user.click(screen.getByRole('button',{name:'返回管理员'}));await screen.findByText('代登录操作失败');expect(navigation).not.toHaveBeenCalled();expect(client.getQueryData(['apps'])).toEqual({private:'member-account'});
});
it('includes the support context in the authenticated identity without changing the member role',async()=>{
 vi.mocked(fetch).mockResolvedValueOnce(json({user:member,impersonation:scope,csrf_token:'member-csrf'}));const identity=await authProvider.getIdentity!({});expect(identity).toMatchObject({id:member.id,role:'member',fullName:member.name,impersonation:scope});
});
