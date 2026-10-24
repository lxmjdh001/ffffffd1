import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { ChartNoAxesCombined, Globe2, Monitor, Users, Eye } from 'lucide-react';
import { api } from './api';
import { localizedCountry, t, useLocale } from './i18n';
import { Badge, date, Empty, ErrorState, Modal, number, Pager, Spinner } from './ui';
import { countryOptions } from './cloak-options';
import './visit-statistics.css';

type Range = 'today' | '7d' | '30d';
type Kind = 'apps' | 'pwas' | 'cloaks' | 'traffic';
export type VisitStatistics = {
 range:Range; allowed_ranges:Range[]; timezone:string; start_at:string; end_at:string;
 summary:{views:number;unique_ips:number;allowed:number;blocked:number;unavailable:number;last_visited_at:string|null};
 series:{label:string;views:number;unique_ips:number}[];
 countries:{name:string;count:number}[]; devices:{name:string;count:number}[];
 records:{total:number;data:{id:string;visited_at:string;ip:string;user_agent:string;country:string;device:string;os:string;browser:string;outcome:string}[]};
};
const countryName=(code:string)=>code==='XX'?t('未知国家'):localizedCountry(code,countryOptions.find(option=>option.value===code)?.label||code);
const rangeName=(range:Range)=>range==='today'?t('今日'):range==='7d'?t('7天'):t('近一月');
function VisitTrend({series}:{series:VisitStatistics['series']}) {
 const max=Math.max(2,...series.map(p=>p.views));const step=660/series.length;const bar=Math.max(3,step*.55);const interval=Math.ceil(series.length/6);
 return <section className="visit-trend"><div className="visit-section-title"><h3>{t('访问趋势')}</h3><span><i/>{t('访问次数')}</span></div><svg viewBox="0 0 720 196" role="img" aria-label={t('访问趋势')}><title>{t('各时间段访问次数与独立 IP')}</title>{[0,.5,1].map(n=><g key={n}><line x1="42" y1={150-n*124} x2="710" y2={150-n*124}/><text x="32" y={154-n*124} textAnchor="end">{number(Math.round(max*n))}</text></g>)}{series.map((point,i)=>{const height=point.views/max*124;const x=50+i*step;return <g key={point.label}><rect x={x} y={150-height} width={bar} height={height} rx="3" tabIndex={0} aria-label={`${point.label} · ${t('访问次数')} ${number(point.views)} · ${t('独立 IP')} ${number(point.unique_ips)}`}><title>{point.label} · {t('访问次数')} {number(point.views)} · {t('独立 IP')} {number(point.unique_ips)}</title></rect>{(i%interval===0||i===series.length-1)&&<text x={x+bar/2} y="179" textAnchor="middle">{point.label.length>5?point.label.slice(5):point.label}</text>}</g>})}</svg></section>;
}
function Breakdown({title,items,country=false}:{title:string;items:{name:string;count:number}[];country?:boolean}) {
 const max=Math.max(1,...items.map(v=>v.count));return <section className="visit-breakdown"><h3>{title}</h3>{!items.length?<p>{t('暂无数据')}</p>:items.map(v=><div className="visit-breakdown-item" key={v.name}><div><span>{country?countryName(v.name):t(v.name)||t('未知设备')}</span><strong>{number(v.count)}</strong></div><div className="visit-breakdown-track"><span style={{width:`${v.count/max*100}%`}}/></div></div>)}</section>;
}
function VisitStatisticsDialog({kind,id,name,close}:{kind:Kind;id:string;name:string;close:()=>void}) {
 useLocale();const [range,setRange]=useState<Range>('today');const [page,setPage]=useState(1);
 const {data,error,refetch}=useQuery({queryKey:['visit-statistics',kind,id,range,page],queryFn:({signal})=>api<VisitStatistics>(`/visit-statistics/${kind}/${encodeURIComponent(id)}?range=${range}&page=${page}`,{signal}),refetchOnMount:'always'});
 const [ranges,setRanges]=useState<Range[]>(['today','7d']);
 useEffect(()=>{if(data)setRanges(data.allowed_ranges);},[data]);
 return <Modal title={t('访问统计')} close={close} wide><div className="visit-statistics"><div className="visit-statistics-head"><div><strong>{name}</strong><p>{data?`${date(data.start_at)} — ${date(data.end_at)}`:t('今日访问统计')}<span>{t('北京时间')}</span></p></div><div className="visit-range-tabs" role="group" aria-label={t('统计时间')}>{ranges.map(value=><button type="button" key={value} aria-pressed={range===value} onClick={()=>{setRange(value);setPage(1);}}>{rangeName(value)}</button>)}</div></div>{error?<ErrorState error={error} retry={()=>refetch()}/>:!data?<Spinner/>:<><div className={`visit-metrics ${kind==='cloaks'?'four':''}`}><article><span><Eye size={17}/>{t('访问次数')}</span><strong>{number(data.summary.views)}</strong><small>PV</small></article><article><span><Users size={17}/>{t('独立 IP')}</span><strong>{number(data.summary.unique_ips)}</strong><small>{t('所选时间内去重')}</small></article>{kind==='cloaks'&&<><article><span>{t('放行次数')}</span><strong>{number(data.summary.allowed)}</strong><small>{t('通过访问规则')}</small></article><article><span>{t('拦截次数')}</span><strong>{number(data.summary.blocked)}</strong><small>{t('进入安全页')}</small></article></>}</div><p className="visit-last">{t('最近访问')}：{date(data.summary.last_visited_at,true)}</p><VisitTrend series={data.series}/><div className="visit-breakdowns"><Breakdown title={t('国家分布')} items={data.countries} country/><Breakdown title={t('设备分布')} items={data.devices}/></div><section className="visit-records"><div className="visit-section-title"><h3>{t('访问明细')}</h3><span>{number(data.records.total)} {t('条记录')}</span></div>{!data.records.data.length?<Empty title={t('所选时间暂无访问记录')} detail={t('新的访问将在这里展示。')}/>:<div className="table-wrap"><table><thead><tr><th>{t('访问时间')}</th><th>{t('IP / 设备')}</th><th>{t('国家')}</th><th>{t('访问结果')}</th></tr></thead><tbody>{data.records.data.map(row=><tr key={row.id}><td>{date(row.visited_at,true)}</td><td><strong className="mono">{row.ip||'—'}</strong><small className="cell-sub"><Monitor size={12}/> {[t(row.device),t(row.os),t(row.browser)].filter(Boolean).join(' · ')}</small><details className="visit-ua"><summary>{t('查看 UA')}</summary><p>{row.user_agent||'—'}</p></details></td><td><Globe2 size={13}/> {countryName(row.country)}</td><td><Badge tone={row.outcome==='blocked'?'red':row.outcome==='allowed'?'green':'gray'}>{row.outcome==='blocked'?t('已拦截'):row.outcome==='allowed'?t('已放行'):t('暂不可用')}</Badge></td></tr>)}</tbody></table></div>}<Pager page={page} total={data.records.total} setPage={setPage}/></section><p className="visit-note">{t('访问次数记录页面或入口请求，独立 IP 按所选时间范围去重。')}</p></>}</div></Modal>;
}
export function VisitStatisticsButton({kind,id,name}:{kind:Kind;id:string;name:string}) {
 const [open,setOpen]=useState(false);return <><button type="button" className="text-btn" aria-label={t('查看{0}访问统计',{0:name})} onClick={()=>setOpen(true)}><ChartNoAxesCombined size={14}/>{t('统计')}</button>{open&&<VisitStatisticsDialog kind={kind} id={id} name={name} close={()=>setOpen(false)}/>}</>;
}
