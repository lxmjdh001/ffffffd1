import { render, screen, cleanup } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { UploadDialog } from '../src/apps';
import { upload } from '../src/api';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

vi.mock('../src/api',async(importOriginal)=>{const original=await importOriginal<typeof import('../src/api')>();return {...original,upload:vi.fn(async()=>({id:'test-app'}))};});
beforeEach(()=>{
 Object.defineProperty(HTMLDialogElement.prototype,'showModal',{configurable:true,value:function(this:HTMLDialogElement){this.setAttribute('open','');}});
 Object.defineProperty(HTMLDialogElement.prototype,'close',{configurable:true,value:function(this:HTMLDialogElement){this.removeAttribute('open');}});
});
afterEach(()=>{cleanup();vi.clearAllMocks();});
it('sends an optional custom share identifier with the app upload',async()=>{
 const user=userEvent.setup();const done=vi.fn();
 render(<ConfigContext.Provider value={{config:{development:true,max_upload_bytes:10*1024*1024} as Config,reload:vi.fn()}}><UploadDialog close={vi.fn()} done={done}/></ConfigContext.Provider>);
 await user.upload(document.querySelector('input[type=file]') as HTMLInputElement,new File(['fixture'],'sample.apk',{type:'application/vnd.android.package-archive'}));
 await user.type(screen.getByRole('textbox',{name:/分享标识/}),'My-Brand');
 await user.click(screen.getByRole('button',{name:'上传并发布'}));
 expect(upload).toHaveBeenCalled();const form=vi.mocked(upload).mock.calls[0][1];expect(form.get('slug')).toBe('my-brand');expect(done).toHaveBeenCalled();
});
