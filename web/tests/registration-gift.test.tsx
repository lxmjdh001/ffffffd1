import { cleanup, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, expect, it, vi } from 'vitest';
import userEvent from '@testing-library/user-event';
import { RegistrationGift } from '../src/registration-gift';
import { ConfigContext } from '../src/ui';
import type { Config } from '../src/types';

afterEach(()=>{cleanup();sessionStorage.clear();vi.unstubAllGlobals();});
function setup(status=401, registration_open=true, amount=100e6){
 vi.stubGlobal('fetch',vi.fn(async()=>new Response(JSON.stringify(status===200?{user:{id:'user'}}:{error:'Unauthorized'}),{status})));
 render(<MemoryRouter><ConfigContext.Provider value={{config:{registration_open,registration_gift_micros:amount} as Config,reload:vi.fn()}}><RegistrationGift registerPath="/register?invite=test"/></ConfigContext.Provider></MemoryRouter>);
}
it('shows configured credit to guests and preserves the invitation in the register link',async()=>{
 setup();
 const card=await screen.findByRole('complementary',{name:'注册赠送用量额度'},{timeout:2500});
 expect(card.textContent).toContain('$100');
 expect(screen.getByRole('link',{name:'免费创建账户'}).getAttribute('href')).toBe('/register?invite=test');
 await userEvent.click(screen.getByRole('button',{name:'关闭'}));
 expect(screen.queryByRole('complementary')).toBeNull();
 expect(sessionStorage.getItem('d1.registration-gift.dismissed')).toBe('1');
});
it('does not show registration offers to authenticated users',async()=>{
 setup(200);await waitFor(()=>expect(fetch).toHaveBeenCalled());expect(screen.queryByRole('complementary')).toBeNull();
});
it('does not check identity or show an offer when registration is closed',()=>{
 setup(401,false);expect(fetch).not.toHaveBeenCalled();expect(screen.queryByRole('complementary')).toBeNull();
});
