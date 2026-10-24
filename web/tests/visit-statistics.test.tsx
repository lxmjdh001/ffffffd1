import {cleanup,render,screen,waitFor,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {QueryClient,QueryClientProvider} from '@tanstack/react-query';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {VisitStatisticsButton,type VisitStatistics} from '../src/visit-statistics';
let paid=false;let rejectMonth=false;let empty=false;let requests:string[]=[];let clients:QueryClient[]=[];
const payload=(range:string,page=1):VisitStatistics=>({range:range as VisitStatistics['range'],allowed_ranges:paid?['today','7d','30d']:['today','7d'],timezone:'Asia/Shanghai',start_at:'2026-10-24T00:00:00Z',end_at:'2026-10-24T01:00:00Z',summary:{views:empty?0:range==='today'?23:range==='7d'?70:300,unique_ips:empty?0:8,allowed:20,blocked:3,unavailable:0,last_visited_at:empty?null:'2026-10-24T01:00:00Z'},series:Array.from({length:range==='today'?24:range==='7d'?7:30},(_,i)=>({label:range==='today'?`${String(i).padStart(2,'0')}:00`:`2026-10-${i+1}`,views:empty?0:i+1,unique_ips:empty?0:1})),countries:empty?[]:[{name:'JP',count:23}],devices:empty?[]:[{name:'手机',count:23}],records:{total:empty?0:23,data:empty?[]:Array.from({length:page===1?20:3},(_,i)=>({id:`${page}-${i}`,visited_at:'2026-10-24T01:00:00Z',ip:`203.0.113.${page*20+i}`,user_agent:'Mozilla/5.0 Full unique test UA',country:'JP',device:'手机',os:'Android',browser:'Chrome',outcome:i===0?'blocked':'allowed'}))}});
beforeEach(()=>{
 paid=false;rejectMonth=false;empty=false;requests=[];
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});
 vi.stubGlobal('fetch',vi.fn(async(input:unknown)=>{const path=String(input);requests.push(path);const url=new URL(path,'https://d1.example');const range=url.searchParams.get('range')||'today';if(rejectMonth&&range==='30d')return new Response(JSON.stringify({error:'近一月访问统计仅限有效付费会员使用'}),{status:403});return new Response(JSON.stringify(payload(range,Number(url.searchParams.get('page')||1))),{status:200});}));
});
afterEach(()=>{cleanup();clients.forEach(client=>client.clear());clients=[];vi.unstubAllGlobals();vi.restoreAllMocks();});
function open(kind:'apps'|'cloaks'|'traffic'|'pwas'='apps'){
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});clients.push(client);render(<QueryClientProvider client={client}><VisitStatisticsButton kind={kind} id="test-resource" name="Demo App"/></QueryClientProvider>);return userEvent.setup();
}
it('opens today statistics, renders full visitor details and restricts free users to two shortcuts',async()=>{
 const user=open();expect(requests).toEqual([]);await user.click(screen.getByRole('button',{name:'查看Demo App访问统计'}));await screen.findByText('203.0.113.20');expect(requests[0]).toBe('/api/visit-statistics/apps/test-resource?range=today&page=1');expect(screen.getByRole('button',{name:'今日'}).getAttribute('aria-pressed')).toBe('true');expect(screen.queryByRole('button',{name:'近一月'})).toBeNull();expect(screen.getAllByText('手机 · Android · Chrome').length).toBe(20);const details=document.querySelector('details')!;expect(details.open).toBe(false);await user.click(within(details).getByText('查看 UA'));expect(details.open).toBe(true);expect(within(details).getByText('Mozilla/5.0 Full unique test UA')).toBeTruthy();await user.click(screen.getByRole('button',{name:'7天'}));await waitFor(()=>expect(requests.at(-1)).toContain('range=7d&page=1'));expect(await screen.findByText('70')).toBeTruthy();
});
it('exposes a month only for paid users, resets pagination on range changes, and reopens today',async()=>{
 paid=true;const user=open('cloaks');await user.click(screen.getByRole('button',{name:'查看Demo App访问统计'}));await screen.findByRole('button',{name:'近一月'});await user.click(screen.getByRole('button',{name:'下一页'}));await screen.findByText('203.0.113.40');await user.click(screen.getByRole('button',{name:'近一月'}));await screen.findByText('300');expect(requests.at(-1)).toContain('range=30d&page=1');expect(screen.getByText('放行次数')).toBeTruthy();await user.click(screen.getByRole('button',{name:'关闭',exact:true}));expect(screen.queryByRole('dialog')).toBeNull();await user.click(screen.getByRole('button',{name:'查看Demo App访问统计'}));await screen.findByText('203.0.113.20');expect(screen.getByRole('button',{name:'今日'}).getAttribute('aria-pressed')).toBe('true');
});
it('shows a server-side membership denial without showing a cached month',async()=>{
 paid=true;const user=open('pwas');await user.click(screen.getByRole('button',{name:'查看Demo App访问统计'}));await screen.findByRole('button',{name:'近一月'});rejectMonth=true;await user.click(screen.getByRole('button',{name:'近一月'}));await screen.findByText('近一月访问统计仅限有效付费会员使用');expect(screen.queryByText('203.0.113.20')).toBeNull();await user.click(screen.getByRole('button',{name:'今日'}));await screen.findByText('203.0.113.20');
});
it('shows the empty state and uses the routing resource identity',async()=>{
 empty=true;const user=open('traffic');await user.click(screen.getByRole('button',{name:'查看Demo App访问统计'}));await screen.findByText('所选时间暂无访问记录');expect(requests[0]).toContain('/visit-statistics/traffic/test-resource?range=today');expect(screen.getByRole('img',{name:'访问趋势'})).toBeTruthy();expect(screen.getByRole('button',{name:'下一页'}).hasAttribute('disabled')).toBe(true);
});
