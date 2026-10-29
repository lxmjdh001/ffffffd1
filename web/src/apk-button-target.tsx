import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { api } from './api';
import { t } from './i18n';
import { errorText, Field, FormError } from './ui';
import type { PageDesign } from './page-designer';

export type APKButtonTarget = Pick<PageDesign, 'button_action' | 'traffic_pool_id'>;
type Pool = { binding_id: string; pool: { name: string; slug: string; enabled: boolean } };
export function APKButtonTargetEditor({value, change, disabled, appID}: {appID?:string; value: APKButtonTarget; change: (value: APKButtonTarget) => void; disabled: boolean}) {
 const traffic=value.button_action==='traffic';
 const pools=useQuery({queryKey:['apk-button-traffic-pools',appID],queryFn:()=>api<{data:Pool[]}>('/traffic-pools'),enabled:traffic});
 const items=(pools.data?.data||[]).filter(item=>item.binding_id);
 const unavailable=!!value.traffic_pool_id&&!pools.isPending&&!pools.error&&!items.some(item=>item.binding_id===value.traffic_pool_id);
 return <section className="panel padded"><h2>{t('下载按钮设置')}</h2><div className="form-grid">
  <Field label={t('按钮目标')}><select aria-label={t('按钮目标')} disabled={disabled} value={traffic?'traffic':'download'} onChange={event=>change({button_action:event.target.value as 'download'|'traffic',traffic_pool_id:event.target.value==='traffic'?value.traffic_pool_id:undefined})}><option value="download">{t('下载当前应用')}</option><option value="traffic">{t('应用分流')}</option></select></Field>
  {traffic&&<Field label={t('绑定应用分流')} hint={t('按钮将按所选分流的规则选择目标链接。')}><select required data-field="traffic_pool_id" aria-label={t('绑定应用分流')} disabled={disabled} aria-busy={pools.isPending} value={value.traffic_pool_id||''} onChange={event=>change({button_action:'traffic',traffic_pool_id:event.target.value})}><option value="">{pools.isPending?t('正在加载…'):t('请选择要绑定的应用分流')}</option>{unavailable&&<option value={value.traffic_pool_id} disabled>{t('已绑定的分流不可用，请重新选择')}</option>}{items.map(item=><option key={item.binding_id} value={item.binding_id}>{item.pool.name} · {item.pool.slug}{item.pool.enabled?'':` · ${t('已暂停')}`}</option>)}</select></Field>}
 </div>{traffic&&<p className="muted small">{t('使用应用分流时，不启用自定义外部浏览器跳转。')}</p>}{traffic&&pools.error&&<><FormError>{errorText(pools.error)}</FormError><button type="button" className="text-btn" disabled={pools.isFetching} onClick={()=>void pools.refetch()}>{t('重新加载分流')}</button></>}{traffic&&unavailable&&<p className="muted small">{t('已绑定的分流不可用，请重新选择')}</p>}{traffic&&!pools.isPending&&!pools.error&&items.length===0&&<p className="muted small">{t('暂无应用分流，请先创建。')} <Link to="/traffic">{t('前往应用分流')}</Link></p>}</section>;
}
