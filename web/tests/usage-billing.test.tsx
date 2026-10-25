import {useState} from 'react';
import {cleanup,render,screen,within} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {UsagePriceFields,UsageRates,usagePrice} from '../src/usage-billing';
import type {Config,Settings} from '../src/types';
afterEach(cleanup);
it('edits only two rates and a dollar-denominated registration credit',async()=>{
 const initial={download_price_micros:50000,pwa_install_price_micros:50000,cloak_uv_price_micros:0,return_flow_price_micros:0,pwa_push_price_micros:0,registration_gift_limit:0,registration_gift_micros:100e6,app_limit:500,storage_limit_bytes:30*2**30} as Settings;
 let saved=initial;function Editor(){const [settings,set]=useState(initial);saved=settings;return <UsagePriceFields settings={settings} change={(key,value)=>set(previous=>({...previous,[key]:value}))}/>;}
 render(<Editor/>);const user=userEvent.setup();for(const [label,key,value]of [['APK 下载','download_price_micros','0.06'],['PWA 安装','pwa_install_price_micros','0.07']]as const){const input=screen.getByRole('spinbutton',{name:label});await user.clear(input);await user.type(input,value);expect(saved[key]).toBe(Math.round(Number(value)*1e6));}
 expect(screen.queryByRole('spinbutton',{name:'智能斗篷'})).toBeNull();expect(screen.queryByRole('spinbutton',{name:'回流页功能'})).toBeNull();expect(screen.queryByRole('spinbutton',{name:'推送用量'})).toBeNull();
 expect(screen.queryByRole('spinbutton',{name:'应用分流'})).toBeNull();expect(screen.queryByRole('spinbutton',{name:'域名安全检测'})).toBeNull();
 const gift=screen.getByLabelText('注册赠送额度（美元）');await user.clear(gift);await user.type(gift,'80.12');expect(saved.registration_gift_micros).toBe(80120000);
 expect(saved.registration_gift_limit).toBe(0);expect(saved.app_limit).toBe(500);expect(saved.storage_limit_bytes).toBe(30*2**30);
});
it('lists routing and domain safety as free without changing the two paid rates',()=>{
 render(<UsageRates config={{download_price_micros:50000,pwa_install_price_micros:50000,registration_gift_micros:100e6} as Config}/>);
 for(const label of ['应用分流','域名安全检测']){
  const card=screen.getByRole('heading',{name:label,exact:true}).closest('article')!;
  expect(within(card).getByText('免费',{exact:true})).toBeTruthy();
  expect(card.querySelector('small')).toBeNull();
 }
 expect(screen.getAllByText('免费',{exact:true})).toHaveLength(5);
 expect(screen.getAllByText('$0.05',{exact:true})).toHaveLength(2);
 expect(screen.queryByText('域名安全检测免费，账户有可用余额或赠送额度即可开启。')).toBeNull();
 expect(document.body.textContent).not.toContain('120');
});
it('formats configurable prices through six decimals and preserves zero',()=>{
 expect([0,1,1000,3000,30000,1e6].map(usagePrice)).toEqual(['0','0.000001','0.001','0.003','0.03','1']);
});
