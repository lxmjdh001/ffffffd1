import { landingLanguages } from './landing-languages';
import { t } from './i18n';

export function LandingLanguagePicker({languages,active,disabled=false,change,select}:{languages:string[];active:string;disabled?:boolean;change:(codes:string[])=>void;select:(code:string)=>void}) {
 return <><div className="pwa-language-options">{landingLanguages.map(([code,label])=><label key={code}><input type="checkbox" disabled={disabled||(languages.length===1&&languages[0]===code)} checked={languages.includes(code)} onChange={e=>{const next=e.target.checked?[...languages,code]:languages.filter(v=>v!==code);change(next);select(e.target.checked?code:next[0]);}}/>{label}</label>)}</div><div className="pwa-language-tabs">{languages.map((code,index)=><div key={code}><button type="button" disabled={disabled} className={active===code?'active':''} onClick={()=>select(code)}>{landingLanguages.find(([key])=>key===code)?.[1]||code}</button>{index>0&&<button type="button" disabled={disabled} aria-label={t('提高语言优先级')} onClick={()=>{const next=[...languages];[next[index-1],next[index]]=[next[index],next[index-1]];change(next);}}>←</button>}</div>)}</div></>;
}
