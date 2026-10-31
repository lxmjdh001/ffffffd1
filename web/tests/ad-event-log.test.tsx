import { cleanup, render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import { AdsPage } from '../src/ads';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

vi.mock('react-admin', () => ({ useNotify: () => vi.fn() }));
let client: QueryClient;
afterEach(() => { cleanup(); client?.clear(); vi.unstubAllGlobals(); });

it.each(['apps', 'pwas'] as const)('shows the event and public app number in %s logs, with the correct app link', async kind => {
  const events = [
    { id: 'direct', event: kind === 'pwas' ? 'pwa_click' : 'download_start', application_slug: '9d30578e7f2b', verified_by: 'quick_download_server', event_id: 'private-deduplication-id', ip: '203.0.113.1', device:'手机', os:'Android', event_time: '2026-10-30T08:51:37Z', deliveries: [{ id:'delivery', platform:'facebook', status:'sent', attempts:1, event_name:'Lead' }] },
    { id: 'routed', event: 'download_start', application_slug: 'e8d0c8d2d045', downloaded_app_id: 'target-app', verified_by: 'quick_download_server', event_id: 'routed-deduplication-id', ip: '203.0.113.2', device:'桌面', os:'Windows', event_time: '2026-10-30T08:52:37Z', deliveries: [] },
    { id: 'deleted', event: 'download_complete', application_slug: '', downloaded_app_id: 'deleted-app', verified_by: 'download_server', event_id: 'deleted-deduplication-id', ip: '203.0.113.3', event_time: '2026-10-30T08:53:37Z', deliveries: [] },
  ];
  vi.stubGlobal('fetch', vi.fn(async (input: string) => new Response(JSON.stringify(input.includes('/events') ? {data:events,total:events.length} : {config:{mode:'quick'},facebook_token_set:true}))));
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={[`/${kind}/source-app/ads?tab=events`]}><ConfigContext.Provider value={{config:{public_url:'https://example.test'} as Config,reload:vi.fn()}}><Routes><Route path={`/${kind}/:id/ads`} element={<AdsPage kind={kind}/>} /></Routes></ConfigContext.Provider></MemoryRouter></QueryClientProvider>);
  const direct = (await screen.findByRole('link',{name:'9d30578e7f2b'})).closest('td')!;
  expect(within(direct).getByText(kind === 'pwas' ? '点击安装' : '下载')).toBeTruthy();
  expect(within(direct).getByRole('link').getAttribute('href')).toBe(kind === 'pwas' ? '/pwas/source-app/edit' : '/apps/source-app');
  expect(screen.getByRole('link',{name:'e8d0c8d2d045'}).getAttribute('href')).toBe('/apps/target-app');
  expect(screen.getByRole('columnheader',{name:'事件 / 应用'})).toBeTruthy();
  expect(screen.queryByText('开始下载',{exact:true})).toBeNull();
  expect(screen.queryByText('查看实际下载应用')).toBeNull();
  expect(screen.queryByText(/deduplication-id/)).toBeNull();
  expect(screen.getByRole('columnheader',{name:'设备 / 系统'})).toBeTruthy();
  const mobile=screen.getByText('203.0.113.1').closest('tr')!;
  expect(within(mobile).getByText('手机')).toBeTruthy();
  expect(within(mobile).getByText('Android')).toBeTruthy();
  const desktop=screen.getByText('203.0.113.2').closest('tr')!;
  expect(within(desktop).getByText('桌面')).toBeTruthy();
  expect(within(desktop).getByText('Windows')).toBeTruthy();
  expect(screen.getByText('成功回传')).toBeTruthy();
  expect(screen.queryByText('facebook')).toBeNull();
  expect(screen.queryByText('Lead')).toBeNull();
  const deleted = screen.getByText('203.0.113.3').closest('tr')!;
  expect(within(deleted).queryByRole('link')).toBeNull();
  expect(within(deleted).getByText('—')).toBeTruthy();
  expect(within(deleted).getByText('未知设备')).toBeTruthy();
  expect(within(deleted).getByText('未知系统')).toBeTruthy();
});

it.each([
  ['sent','sent','成功回传'],
  ['sent','failed','回传失败'],
  ['sent','pending','回传中'],
  ['sent','processing','回传中'],
])('summarizes mixed platform delivery states %s / %s as %s',async(first,second,label)=>{
  const user=userEvent.setup();
  const event={id:'event',event:'download_start',verified_by:'quick_download_server',ip:'203.0.113.4',device:'手机',os:'iOS',deliveries:[{id:'facebook-delivery',platform:'facebook',status:first,attempts:1,event_name:'Lead'},{id:'tiktok-delivery',platform:'tiktok',status:second,attempts:8,event_name:'Download',last_error:'Provider rejected event'}]};
  vi.stubGlobal('fetch',vi.fn(async(input:string)=>new Response(JSON.stringify(input.includes('/events')?{data:[event],total:1}:{config:{mode:'quick'},facebook_token_set:true}))));
  client=new QueryClient({defaultOptions:{queries:{retry:false}}});
  render(<QueryClientProvider client={client}><MemoryRouter initialEntries={['/apps/app/ads?tab=events']}><ConfigContext.Provider value={{config:{public_url:'https://example.test'} as Config,reload:vi.fn()}}><Routes><Route path="/apps/:id/ads" element={<AdsPage/>}/></Routes></ConfigContext.Provider></MemoryRouter></QueryClientProvider>);
  const row=(await screen.findByText('203.0.113.4')).closest('tr')!;
  expect(within(row).getByText(label)).toBeTruthy();
  expect(within(row).getAllByText(label)).toHaveLength(1);
  expect(within(row).queryByText('Lead')).toBeNull();
  expect(within(row).queryByText('Download')).toBeNull();
  if(second==='failed'){
    await user.click(within(row).getByText('错误详情'));
    expect(within(row).getByText(/Provider rejected event/)).toBeTruthy();
    expect(within(row).getByRole('button',{name:'重新入队'})).toBeTruthy();
  }else{
    expect(within(row).queryByText('回传失败')).toBeNull();
    expect(within(row).queryByText('错误详情')).toBeNull();
  }
});
