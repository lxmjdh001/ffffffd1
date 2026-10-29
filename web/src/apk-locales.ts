import buttons from '../../internal/platform/templates/apk-language-buttons.json';
import type { PageDesign } from './page-designer';

export function apkLocaleCode(code?:string):string { return ({en:'en-US',ja:'ja-JP',ko:'ko-KR',de:'de-DE'} as Record<string,string>)[code||'']||code||'zh-CN'; }
export function apkLocaleButton(code?:string):string { return buttons[apkLocaleCode(code) as keyof typeof buttons]||buttons['en-US']; }
export function plainAPKDesign(d:PageDesign):PageDesign { const {localizations:_,language_priority:__,...locale}=d;return structuredClone(locale); }
export function apkLocaleList(d:PageDesign):PageDesign[] {return d.localizations?.length?d.localizations:[{...plainAPKDesign(d),language:apkLocaleCode(d.language)}];}
export function activeAPKDesign(d:PageDesign,active?:string):PageDesign {const locale=d.localizations?.find(v=>apkLocaleCode(v.language)===active)||d.localizations?.[0];return locale?{...locale,external_browser:d.external_browser,return_flow_config:d.return_flow_config,button_action:d.button_action,traffic_pool_id:d.traffic_pool_id}:d;}
export function replaceAPKLocale(d:PageDesign,active:string|undefined,next:PageDesign):PageDesign {
 if(!d.localizations?.length)return next;
 const code=active||apkLocaleCode(d.localizations[0].language);
 // PWA also keeps return-flow behavior global, outside its language records.
 const localizations=d.localizations.map(v=>({...((apkLocaleCode(v.language)===code)?plainAPKDesign(next):v),external_browser:next.external_browser,return_flow_config:next.return_flow_config,button_action:next.button_action,traffic_pool_id:next.traffic_pool_id}));
 return {...plainAPKDesign(localizations[0]),external_browser:next.external_browser,return_flow_config:next.return_flow_config,button_action:next.button_action,traffic_pool_id:next.traffic_pool_id,localizations,language_priority:d.language_priority};
}
export function changeAPKLanguages(d:PageDesign,codes:string[],active?:string):PageDesign {
 const current=activeAPKDesign(d,active);const previous=apkLocaleList(d);
 const localizations=codes.map(language=>{
  const saved=previous.find(v=>apkLocaleCode(v.language)===language);if(saved)return {...plainAPKDesign(saved),external_browser:d.external_browser,return_flow_config:d.return_flow_config,button_action:d.button_action,traffic_pool_id:d.traffic_pool_id,language};
  const locale=plainAPKDesign(current);const auto=!locale.button_text.trim()||Object.values(buttons).includes(locale.button_text as never);
  return {...locale,language,button_text:auto?apkLocaleButton(language):locale.button_text};
 });
 return {...plainAPKDesign(localizations[0]),external_browser:d.external_browser,return_flow_config:d.return_flow_config,button_action:d.button_action,traffic_pool_id:d.traffic_pool_id,localizations,language_priority:codes};
}
