import {afterEach,expect,it,vi} from 'vitest';
import {reloadAccount} from '../src/account-context';
afterEach(()=>{vi.unstubAllGlobals();localStorage.clear();});
it.each(['/', '/admin/users'] as const)('reloads the whole document at %s instead of only changing the hash',path=>{
 const replaceState=vi.fn(),reload=vi.fn();vi.stubGlobal('window',{history:{replaceState},location:{reload}});
 reloadAccount(path);
 expect(replaceState).toHaveBeenCalledWith(null,'','/#'+path);expect(reload).toHaveBeenCalledOnce();
 const notice=JSON.parse(localStorage.getItem('d1.account-context')!);expect(notice.path).toBe(path);expect(notice.nonce).toBeTruthy();
});
it('reloads another tab without broadcasting the change again',()=>{
 const replaceState=vi.fn(),reload=vi.fn();vi.stubGlobal('window',{history:{replaceState},location:{reload}});
 localStorage.setItem('d1.account-context','existing-notice');reloadAccount('/',false);expect(localStorage.getItem('d1.account-context')).toBe('existing-notice');expect(reload).toHaveBeenCalledOnce();
});
