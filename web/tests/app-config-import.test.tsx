import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { UploadDialog } from '../src/apps';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';
import { upload } from '../src/api';

vi.mock('react-admin',()=>({useNotify:()=>vi.fn()}));
vi.mock('../src/api',async()=>({...await vi.importActual('../src/api'),upload:vi.fn()}));
let client: QueryClient;
beforeEach(()=>{
 client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value(){this.setAttribute('open','');}});
 Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value(){this.removeAttribute('open');}});
 vi.stubGlobal('fetch',vi.fn(async(input:string)=>{
  const row={id:'source',name:'Previous app',version:'3.2',description:'Existing description',file_name:'source.apk'};
  return new Response(JSON.stringify(input.includes('?')?{data:[row],total:1}:row));
 }));
});
afterEach(()=>{cleanup();client.clear();vi.restoreAllMocks();vi.unstubAllGlobals();vi.mocked(upload).mockReset();});
function setup(){
 const done=vi.fn();
 render(<QueryClientProvider client={client}><ConfigContext.Provider value={{config:{max_upload_bytes:10*1024*1024,development:true} as Config,reload:vi.fn()}}><UploadDialog close={vi.fn()} done={done}/></ConfigContext.Provider></QueryClientProvider>);
 return {user:userEvent.setup(),done};
}
it('imports APK metadata but still requires a new package and a fresh share identifier',async()=>{
 const {user,done}=setup();
 await user.click(screen.getByRole('button',{name:'导入已有应用配置'}));
 const dialogs=await screen.findAllByRole('dialog');const picker=within(dialogs[1]);
 await user.click(await picker.findByRole('button',{name:/Previous app.*导入配置/}));
 await waitFor(()=>expect(screen.getAllByRole('dialog')).toHaveLength(1));
 expect((screen.getByLabelText('应用名称') as HTMLInputElement).value).toBe('Previous app');
 expect((screen.getByLabelText('应用说明') as HTMLTextAreaElement).value).toBe('Existing description');
 await user.click(screen.getByRole('button',{name:'上传并发布'}));
 expect(screen.getByRole('alert').textContent).toContain('请选择应用安装包');
 expect(upload).not.toHaveBeenCalled();
 const file=screen.getByRole('dialog').querySelector<HTMLInputElement>('input[type=file]')!;
 await user.upload(file,new File(['APK'],'new.apk',{type:'application/vnd.android.package-archive'}));
 vi.mocked(upload).mockResolvedValue({id:'new'} as never);
 await user.click(screen.getByRole('button',{name:'上传并发布'}));
 await waitFor(()=>expect(done).toHaveBeenCalled());
 const body=vi.mocked(upload).mock.calls[0][1];
 expect(body.get('import_from')).toBe('source');expect(body.get('name')).toBe('Previous app');expect(body.get('slug')).toBeNull();
});
it('jumps to missing fields and to a server-rejected share identifier',async()=>{
 const {user}=setup();
 await user.click(screen.getByRole('button',{name:'上传并发布'}));
 expect(document.activeElement).toBe(screen.getByLabelText('应用名称'));
 await user.type(screen.getByLabelText('应用名称'),'New app');
 const file=screen.getByRole('dialog').querySelector<HTMLInputElement>('input[type=file]')!;
 await user.upload(file,new File(['APK'],'new.apk',{type:'application/vnd.android.package-archive'}));
 await user.type(screen.getByLabelText(/分享标识/),'used-id');
 vi.mocked(upload).mockRejectedValue(new Error('分享标识已被使用，请换一个'));
 await user.click(screen.getByRole('button',{name:'上传并发布'}));
 await waitFor(()=>expect(document.activeElement).toBe(screen.getByLabelText(/分享标识/)));
});
