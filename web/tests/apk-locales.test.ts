import {describe,it,expect} from 'vitest';
import {activeAPKDesign,changeAPKLanguages,replaceAPKLocale} from '../src/apk-locales';
import type {PageDesign} from '../src/page-designer';
describe('APK language data',()=>{
 it('keeps return-flow behavior global like PWA when editing or removing a locale',()=>{
  const base={language:'zh-CN',button_text:'下载应用',return_flow_config:{enabled:false}} as PageDesign;
  const doc=changeAPKLanguages(base,['zh-CN','en-US']);
  const english=activeAPKDesign(doc,'en-US');
  const next=replaceAPKLocale(doc,'en-US',{...english,title:'English only',return_flow_config:{...english.return_flow_config!,enabled:true}});
  expect(activeAPKDesign(next,'zh-CN').title).not.toBe('English only');
  expect(activeAPKDesign(next,'zh-CN').return_flow_config?.enabled).toBe(true);
  expect(changeAPKLanguages(next,['en-US']).return_flow_config?.enabled).toBe(true);
 });
});
