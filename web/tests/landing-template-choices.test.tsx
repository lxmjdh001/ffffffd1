import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { LandingTemplateTabs } from '../src/landing-template-tabs';
import { LandingSourceSelector } from '../src/landing-source-selector';
import { PWALandingEditor, defaultPWALanding } from '../src/pwa-landing';
import { defaultDIYLandingOptions, type DIYLandingReference } from '../src/diy-landing-types';
import { setLocale } from '../src/i18n';

const config=defaultDIYLandingOptions();
const response=(value:unknown,status=200)=>new Response(JSON.stringify(value),{status,headers:{'Content-Type':'application/json'}});
beforeEach(()=>setLocale('zh-CN',false));
afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();});

it('offers only Google Play and DIY cards while retaining an existing old template until explicitly changed',async()=>{
 const change=vi.fn(),landing=defaultPWALanding();landing.template='custom';landing.settings.apkUrl='https://assets.example/app.apk';
 render(<MemoryRouter><PWALandingEditor value={landing} change={change} disabled={false} uploading={vi.fn()} activeLanguage="en-US" selectLanguage={vi.fn()}/></MemoryRouter>);
 const choices=within(screen.getByRole('group',{name:'落地页模板'}));
 expect(choices.getAllByRole('button')).toHaveLength(2);
 expect(choices.queryByRole('button',{name:'自定义图片模板'})).toBeNull();expect(choices.queryByRole('button',{name:'原有安装页'})).toBeNull();
 expect(choices.getByRole('button',{name:'Google Play 商店模板'}).getAttribute('aria-pressed')).toBe('false');
 expect(screen.getByText('当前应用保留旧版模板。选择新模板后才会更换页面。')).toBeTruthy();expect(change).not.toHaveBeenCalled();
 await userEvent.setup().click(choices.getByRole('button',{name:'DIY落地页'}));
 expect(change).toHaveBeenCalledWith(expect.objectContaining({template:'diy',settings:expect.objectContaining({apkUrl:'https://assets.example/app.apk'})}));
});

it('shows Google Play as the selected default and supports keyboard activation',async()=>{
 const change=vi.fn();render(<LandingTemplateTabs value="google_play" onChange={change}/>);
 const google=screen.getByRole('button',{name:'Google Play 商店模板'}),diy=screen.getByRole('button',{name:'DIY落地页'});
 expect(google.getAttribute('aria-pressed')).toBe('true');expect(diy.getAttribute('aria-pressed')).toBe('false');
 diy.focus();await userEvent.setup().keyboard('{Enter}');expect(change).toHaveBeenCalledWith('diy');
});

it('selects a fixed favorite reference and keeps favorite snapshots out of the personal DIY list',async()=>{
 const requests:string[]=[],change=vi.fn();
 vi.stubGlobal('fetch',vi.fn(async(input:string)=>{
  const path=String(input);requests.push(path);
  if(path.includes('/landing-favorites?'))return response({data:[{id:'library-one',name:'管理员活动页',reference:{page_id:'favorite-page',revision_id:'favorite-revision'},version:3,config}],total:1});
  if(path.includes('/diy-landings?'))return response({data:[{id:'favorite-page',name:'管理员活动页',latest_published_revision_id:'favorite-revision',draft_config:config},{id:'personal-page',name:'自己的活动页',latest_published_revision_id:'personal-revision',draft_config:config}],total:2});
  return response({id:'favorite-page',name:'管理员活动页',revisions:[{id:'favorite-revision',revision_no:1,config,published_at:'2026-10-26T00:00:00Z'}]});
 }));
 function Selector(){const[value,setValue]=useState<DIYLandingReference>();return <MemoryRouter><LandingSourceSelector kind="pwa" value={value} onChange={next=>{setValue(next);change(next);}} hidePreview/></MemoryRouter>;}
 render(<Selector/>);const user=userEvent.setup();
 await waitFor(()=>expect(screen.getByLabelText('已收藏落地页').getAttribute('disabled')).toBeNull());
 expect(screen.getByRole('tab',{name:/我的收藏/}).getAttribute('aria-selected')).toBe('true');
 await user.selectOptions(screen.getByLabelText('已收藏落地页'),'favorite-page');
 expect(change).toHaveBeenLastCalledWith({page_id:'favorite-page',revision_id:'favorite-revision'});
 expect(await screen.findByText('使用收藏时的固定版本，模版库更新不会改变当前应用。')).toBeTruthy();
 await user.click(screen.getByRole('tab',{name:/我的DIY/}));
 const own=screen.getByLabelText('已发布落地页');
 expect(within(own).getByRole('option',{name:/自己的活动页/})).toBeTruthy();
 // The current reference is retained as a placeholder, not offered again as a second owned item.
 expect(within(own).getAllByRole('option').filter(option=>option.textContent?.includes('管理员活动页'))).toHaveLength(1);
 expect(screen.getByRole('link',{name:'打开DIY落地页'}).getAttribute('target')).toBe('_blank');
 expect(requests.some(path=>path.includes('/landing-favorites?target=pwa&per_page=200'))).toBe(true);
});

it('preserves a bound page after it leaves favorites and allows browsing the library in a new tab',async()=>{
 const change=vi.fn();
 vi.stubGlobal('fetch',vi.fn(async(input:string)=>String(input).includes('?')?response({data:[],total:0}):response({name:'原收藏页面',archived_at:'2026-10-26T00:00:00Z',revisions:[{id:'saved-v1',revision_no:1,config,published_at:'2026-10-26T00:00:00Z'}]})));
 render(<MemoryRouter><LandingSourceSelector kind="apk" value={{page_id:'saved',revision_id:'saved-v1'}} onChange={change} hidePreview/></MemoryRouter>);
 await waitFor(()=>expect(screen.getByLabelText('已发布落地页').getAttribute('disabled')).toBeNull());
 expect((screen.getByLabelText('已发布落地页') as HTMLSelectElement).value).toBe('saved');
 expect(await screen.findByText('此落地页已归档，当前应用可继续使用，不能新增绑定。')).toBeTruthy();expect(change).not.toHaveBeenCalled();
 await userEvent.setup().click(screen.getByRole('tab',{name:/我的收藏/}));
 const library=screen.getByRole('link',{name:'浏览模版库'});expect(library.getAttribute('target')).toBe('_blank');expect(library.getAttribute('href')).toBe('/landing-templates');
 const tabs=screen.getByRole('tablist',{name:'DIY页面来源'});fireEvent.keyDown(tabs,{key:'ArrowRight'});expect(screen.getByRole('tab',{name:/我的DIY/}).getAttribute('aria-selected')).toBe('true');
});

it('clears per-application content when choosing a different page or version',async()=>{
 const change=vi.fn();
 vi.stubGlobal('fetch',vi.fn(async(input:string)=>{
  const path=String(input);
  if(path.includes('/content-fields?'))return response({fields:[]});
  if(path.includes('/landing-favorites?'))return response({data:[],total:0});
  if(path.includes('/diy-landings?'))return response({data:[{id:'personal-page',name:'自己的页面',latest_published_revision_id:'new-v2',draft_config:config}],total:1});
  return response({name:'自己的页面',revisions:[{id:'old-v1',revision_no:1,config,published_at:'2026-10-26T00:00:00Z'},{id:'new-v2',revision_no:2,config,published_at:'2026-10-26T00:00:00Z'}]});
 }));
 function Selector(){const[value,setValue]=useState<DIYLandingReference>({page_id:'personal-page',revision_id:'old-v1',content:{'text-0':'旧版本专属标题'}});return <MemoryRouter><LandingSourceSelector kind="pwa" value={value} onChange={next=>{next&&setValue(next);change(next);}} hidePreview/></MemoryRouter>;}
 render(<Selector/>);const user=userEvent.setup();
 await waitFor(()=>expect((screen.getByLabelText('使用版本') as HTMLSelectElement).disabled).toBe(false));
 await user.selectOptions(screen.getByLabelText('使用版本'),'new-v2');
 expect(change).toHaveBeenLastCalledWith({page_id:'personal-page',revision_id:'new-v2'});
});
