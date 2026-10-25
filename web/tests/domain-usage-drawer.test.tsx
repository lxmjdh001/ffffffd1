import { cleanup,render,screen } from '@testing-library/react';
import { QueryClient,QueryClientProvider } from '@tanstack/react-query';
import { afterEach,expect,it,vi } from 'vitest';
import { DomainShareDrawer } from '../src/domain-share-drawer';

afterEach(()=>{cleanup();vi.unstubAllGlobals();vi.restoreAllMocks();});
it('does not reuse traffic-only domains for APK, PWA or cloak drawers',async()=>{
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});
 Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});
 const long={url:'https://facebook-google-down.godownd1.com',remark:'',source:'platform',audience:'all'};
 const short={...long,url:'https://s.d1url.com'};
 const fetcher=vi.fn(async(input:RequestInfo|URL)=>new Response(JSON.stringify({data:String(input).endsWith('?usage=traffic')?[long,short]:[long]})));
 vi.stubGlobal('fetch',fetcher);
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 const content=(path:string)=><QueryClientProvider client={client}><DomainShareDrawer path={path} close={()=>{}}/></QueryClientProvider>;
 const view=render(content('/r/entry'));await screen.findByText(short.url);
 for(const path of ['/a/app','/p/pwa/','/c/cloak']){
  view.rerender(content(path));await screen.findByText(long.url);
  expect(screen.queryByText(short.url)).toBeNull();
 }
 view.rerender(content('/r/entry'));await screen.findByText(short.url);
 expect(fetcher.mock.calls.some(([url])=>String(url)==='/api/download-domains?usage=traffic')).toBe(true);
 expect(fetcher.mock.calls.some(([url])=>String(url)==='/api/download-domains')).toBe(true);
 client.clear();
});
