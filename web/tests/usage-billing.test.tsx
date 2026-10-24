import {useState} from 'react';
import {cleanup,render,screen} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,expect,it} from 'vitest';
import {UsagePriceFields,usagePrice} from '../src/usage-billing';
import type {Settings} from '../src/types';
afterEach(cleanup);
it('edits all five administrator rates in integer micros without altering other values',async()=>{
 const initial={download_price_micros:3000,pwa_install_price_micros:30000,cloak_uv_price_micros:1000,return_flow_price_micros:1000,pwa_push_price_micros:1000,registration_gift_limit:1000,app_limit:500,storage_limit_bytes:30*2**30} as Settings;
 let saved=initial;function Editor(){const [settings,set]=useState(initial);saved=settings;return <UsagePriceFields settings={settings} change={(key,value)=>set(previous=>({...previous,[key]:value}))}/>;}
 render(<Editor/>);const user=userEvent.setup();for(const [label,key,value]of [['APK 访问 UV','download_price_micros','0.004'],['PWA 安装','pwa_install_price_micros','0.04'],['斗篷访问 UV','cloak_uv_price_micros','0.002'],['回流页展示','return_flow_price_micros','0.003'],['PWA 消息推送','pwa_push_price_micros','0.000001']]as const){const input=screen.getByRole('spinbutton',{name:label});await user.clear(input);await user.type(input,value);expect(saved[key]).toBe(Math.round(Number(value)*1e6));}
 expect(saved.registration_gift_limit).toBe(1000);expect(saved.app_limit).toBe(500);expect(saved.storage_limit_bytes).toBe(30*2**30);
});
it('formats configurable prices through six decimals and preserves zero',()=>{
 expect([0,1,1000,3000,30000,1e6].map(usagePrice)).toEqual(['0','0.000001','0.001','0.003','0.03','1']);
});
