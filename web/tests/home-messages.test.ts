import { expect, it } from 'vitest';
import { resolve } from 'node:path';
import { initialMessageKeys } from '../scripts/home-messages';
import homeContent from '../src/home-content.json';
it('keeps public copy in the initial dictionary and defers console-only strings',()=>{
 const keys=initialMessageKeys(resolve('.'));
 for(const key of ['创建账户','联系商务','立即开始','安装包报毒安全检测',homeContent.faq.heading]) expect(keys.has(key)).toBe(true);
 expect(keys.has('创建充值订单')).toBe(false);
 const walk=(value:unknown)=>{if(typeof value==='string') expect(keys.has(value)).toBe(true);else if(value&&typeof value==='object')Object.values(value).forEach(walk);};
 walk(homeContent);
});
