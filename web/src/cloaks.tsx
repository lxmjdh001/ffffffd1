import { useState, type FormEvent, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { ArrowDown, ArrowLeft, ArrowRight, Check, ChevronRight, CircleHelp, Globe2, Languages, Link2, ListFilter, Monitor, Plus, Save, Search, ShieldCheck, ShieldOff, SlidersHorizontal, Smartphone, Tablet, X } from 'lucide-react';
import { api, send } from './api';
import { Badge, CopyButton, date, Empty, ErrorState, errorText, Field, FormError, Modal, PageHeader, Spinner } from './ui';
import { countryOptions, deviceOptions, languageOptions, platformOptions, type CloakOption } from './cloak-options';
import './cloaks.css';

type Device = 'desktop' | 'tablet' | 'mobile';
type DeviceRule = { device: Device; systems: string[] };
export type CloakDraft = {
  name: string; enabled: boolean; target_type: 'url' | 'traffic_pool'; target_url: string; traffic_pool_slug: string; safe_url: string;
  blocked_languages: string[]; blocked_uas: string[]; blocked_countries: string[]; allowed_platforms: string[]; blocked_devices: DeviceRule[];
};
export type Cloak = CloakDraft & { id: string; slug: string; entry_url: string; created_at: string; updated_at: string };
type CloaksResponse = { data: Cloak[]; country_detection_configured: boolean };
type TrafficChoice = { pool: { slug: string; name: string; enabled: boolean }; entry_url: string };
type RuleTab = 'languages' | 'ua' | 'countries' | 'platforms' | 'devices';
const ruleTabs: Array<{ id: RuleTab; label: string; icon: typeof Languages }> = [
  { id: 'languages', label: '语言拦截', icon: Languages }, { id: 'ua', label: 'UA 拦截', icon: ListFilter }, { id: 'countries', label: '国家拦截', icon: Globe2 }, { id: 'platforms', label: '平台允许', icon: Link2 }, { id: 'devices', label: '设备与系统', icon: Monitor },
];
const newDraft = (): CloakDraft => ({ name: '', enabled: true, target_type: 'traffic_pool', target_url: '', traffic_pool_slug: '', safe_url: '', blocked_languages: [], blocked_uas: [], blocked_countries: [], allowed_platforms: [], blocked_devices: [] });
const editable = (item: CloakDraft): CloakDraft => ({ name: item.name, enabled: item.enabled, target_type: item.target_type, target_url: item.target_url, traffic_pool_slug: item.traffic_pool_slug, safe_url: item.safe_url, blocked_languages: item.blocked_languages, blocked_uas: item.blocked_uas, blocked_countries: item.blocked_countries, allowed_platforms: item.allowed_platforms, blocked_devices: item.blocked_devices });
const toggleValue = (values: string[], value: string, selected: boolean) => selected ? [...values.filter(item => item.toLowerCase() !== value.toLowerCase()), value] : values.filter(item => item.toLowerCase() !== value.toLowerCase());
const labelFor = (options: CloakOption[], value: string) => options.find(option => option.value.toLowerCase() === value.toLowerCase())?.label || value;
const ruleCount = (item: CloakDraft) => [item.blocked_languages.length, item.blocked_uas.length, item.blocked_countries.length, item.allowed_platforms.length, item.blocked_devices.length].filter(Boolean).length;
const deviceIcons = { desktop: Monitor, tablet: Tablet, mobile: Smartphone };

function ChoiceGrid({ label, options, selected, onChange, searchable = false }: { label: string; options: CloakOption[]; selected: string[]; onChange: (values: string[]) => void; searchable?: boolean }) {
  const [search, setSearch] = useState('');
  const visible = options.filter(option => `${option.label} ${option.value}`.toLowerCase().includes(search.trim().toLowerCase()));
  return <div className="cloak-choice-picker">
    <div className="cloak-picker-toolbar">{searchable ? <label className="cloak-search"><Search size={15} /><input aria-label={`搜索${label}`} placeholder={`搜索${label}或代码`} value={search} onChange={event => setSearch(event.target.value)} /></label> : <span>勾选需要拦截的{label}</span>}<button type="button" className="text-btn" disabled={!selected.length} onClick={() => onChange([])}>清空选择</button></div>
    {!!selected.length && <div className="cloak-selected-values" aria-label={`已选${label}`}>{selected.map(value => <button type="button" key={value} onClick={() => onChange(selected.filter(item => item !== value))} aria-label={`移除${labelFor(options, value)}`}>{labelFor(options, value)}<X size={12} /></button>)}</div>}
    <div className="cloak-choice-grid" role="group" aria-label={label}>{visible.map(option => { const checked = selected.some(value => value.toLowerCase() === option.value.toLowerCase()); return <label className={`cloak-choice ${checked ? 'selected' : ''}`} key={option.value}><input type="checkbox" checked={checked} onChange={event => onChange(toggleValue(selected, option.value, event.target.checked))} /><span>{option.label}<small>{option.value}</small></span></label>; })}{!visible.length && <p className="cloak-no-results">没有找到匹配的{label}，试试名称或代码。</p>}</div>
  </div>;
}

function EditorSection({ step, title, description, children }: { step: string; title: string; description: string; children: ReactNode }) {
  return <section className="cloak-editor-section"><header><span className="cloak-step-number">{step}</span><div><h2>{title}</h2><p>{description}</p></div></header>{children}</section>;
}

export function CloaksPage() {
  const { data, error, refetch } = useQuery({ queryKey: ['cloaks'], queryFn: () => api<CloaksResponse>('/cloaks') });
  const pools = useQuery({ queryKey: ['traffic-pools'], queryFn: () => api<{ data: TrafficChoice[] }>('/traffic-pools') });
  const [draft, setDraft] = useState<CloakDraft>();
  const [editing, setEditing] = useState<Cloak>();
  const [deleting, setDeleting] = useState<Cloak>();
  const [activeTab, setActiveTab] = useState<RuleTab>('languages');
  const [uaText, setUaText] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [saved, setSaved] = useState<Cloak>();
  const start = (item?: Cloak) => {
    setEditing(item); setDraft(item ? editable(item) : newDraft());
    setUaText(item?.blocked_uas.join('\n') || ''); setActiveTab('languages'); setMessage(''); setSaved(undefined);
  };
  const update = <K extends keyof CloakDraft>(key: K, value: CloakDraft[K]) => setDraft(current => current ? { ...current, [key]: value } : current);
  const save = async (event: FormEvent) => {
    event.preventDefault(); if (!draft) return; setBusy(true); setMessage('');
    try {
      const payload: CloakDraft = { ...editable(draft), name: draft.name.trim(), target_url: draft.target_type === 'url' ? draft.target_url.trim() : '', traffic_pool_slug: draft.target_type === 'traffic_pool' ? draft.traffic_pool_slug : '', safe_url: draft.safe_url.trim(), blocked_uas: uaText.split(/\r?\n/).map(value => value.trim()).filter(Boolean) };
      const result = await send<Cloak>(editing ? `/cloaks/${editing.id}` : '/cloaks', payload, editing ? 'PUT' : 'POST');
      setSaved(result); setDraft(undefined); setEditing(undefined); await refetch();
    } catch (error) { setMessage(errorText(error)); } finally { setBusy(false); }
  };
  const toggle = async (item: Cloak) => {
    setBusy(true); setMessage(''); setSaved(undefined);
    try { await send(`/cloaks/${item.id}`, { ...editable(item), enabled: !item.enabled }, 'PUT'); await refetch(); }
    catch (error) { setMessage(errorText(error)); } finally { setBusy(false); }
  };
  const remove = async () => {
    if (!deleting) return; setBusy(true); setMessage('');
    try { await api(`/cloaks/${deleting.id}`, { method: 'DELETE' }); setDeleting(undefined); setSaved(undefined); await refetch(); }
    catch (error) { setMessage(errorText(error)); } finally { setBusy(false); }
  };
  const setDeviceSystems = (device: Device, systems: string[]) => update('blocked_devices', [...(draft?.blocked_devices || []).filter(rule => rule.device !== device), ...(systems.length ? [{ device, systems }] : [])]);
  const changeSystem = (device: Device, system: string, checked: boolean) => {
    const options = deviceOptions.find(option => option.value === device)!.systems.map(option => option.value);
    if (system === 'all') { setDeviceSystems(device, checked ? ['all'] : []); return; }
    const current = draft?.blocked_devices.find(rule => rule.device === device)?.systems || [];
    const next = toggleValue(current.includes('all') ? options : current, system, checked);
    setDeviceSystems(device, next.length === options.length ? ['all'] : next);
  };
  if (error) return <ErrorState error={error} retry={() => refetch()} />;
  if (!data) return <Spinner />;
  const trafficChoices = pools.data?.data || [];

  if (draft) {
    const counts: Record<RuleTab, number> = { languages: draft.blocked_languages.length, ua: uaText.split(/\r?\n/).filter(value => value.trim()).length, countries: draft.blocked_countries.length, platforms: draft.allowed_platforms.length, devices: draft.blocked_devices.length };
    const target = draft.target_type === 'url' ? draft.target_url : trafficChoices.find(item => item.pool.slug === draft.traffic_pool_slug)?.entry_url;
    return <div className="cloak-page">
      <div className="cloak-editor-back"><button type="button" onClick={() => { setDraft(undefined); setEditing(undefined); setMessage(''); }} disabled={busy}><ArrowLeft size={15} />返回斗篷列表</button><span>访问规则工作台</span></div>
      <PageHeader eyebrow="CLOAK ROUTING" title={editing ? '编辑斗篷' : '新建斗篷'} subtitle="先选择目标，再设置访问条件，最后指定安全页。" />
      <form onSubmit={save} className="cloak-editor-layout"><div className="cloak-editor-main">
        <EditorSection step="01" title="目标链接" description="未命中拦截条件的访问，将进入这里。">
          <Field label="斗篷名称"><input required maxLength={60} placeholder="例如：海外应用推广" value={draft.name} onChange={event => update('name', event.target.value)} /></Field>
          <div className="cloak-target-types" role="group" aria-label="目标类型">
            <label className={draft.target_type === 'traffic_pool' ? 'selected' : ''}><input type="radio" name="cloak-target-type" value="traffic_pool" checked={draft.target_type === 'traffic_pool'} onChange={() => update('target_type', 'traffic_pool')} /><span className="cloak-type-icon"><SlidersHorizontal size={20} /></span><span><strong>选择分流链接</strong><small>沿用已有分流的分配规则</small></span><span className="cloak-radio-dot" /></label>
            <label className={draft.target_type === 'url' ? 'selected' : ''}><input type="radio" name="cloak-target-type" value="url" checked={draft.target_type === 'url'} onChange={() => update('target_type', 'url')} /><span className="cloak-type-icon"><Link2 size={20} /></span><span><strong>直接输入 URL</strong><small>跳转到指定的网页或下载地址</small></span><span className="cloak-radio-dot" /></label>
          </div>
          {draft.target_type === 'url' ? <Field label="目标 URL"><input type="url" required maxLength={4096} placeholder="https://example.com/download" value={draft.target_url} onChange={event => update('target_url', event.target.value)} /></Field> : <div><Field label="分流链接"><select required value={draft.traffic_pool_slug} onChange={event => update('traffic_pool_slug', event.target.value)}><option value="">{pools.isPending ? '正在读取分流…' : '请选择分流链接'}</option>{trafficChoices.map(item => <option key={item.pool.slug} value={item.pool.slug}>{item.pool.name || item.pool.slug}{!item.pool.enabled ? '（已关闭）' : ''}</option>)}</select></Field>{pools.error ? <div className="cloak-inline-message">分流读取失败。<button type="button" className="text-btn" onClick={() => pools.refetch()}>重试</button></div> : !pools.isPending && !trafficChoices.length ? <p className="cloak-field-help">还没有分流链接。可以<Link to="/traffic">创建分流</Link>，或切换为直接输入 URL。</p> : target && <div className="cloak-target-preview"><Link2 size={14} /><span>{target}</span></div>}</div>}
        </EditorSection>

        <EditorSection step="02" title="访问规则" description="语言、UA、国家和设备均为选中即拦截；平台可单独设置允许条件。">
          <div className="cloak-rule-tabs" role="tablist" aria-label="访问规则" onKeyDown={event => {
            const index = ruleTabs.findIndex(tab => tab.id === activeTab);
            const next = event.key === 'ArrowRight' ? (index + 1) % ruleTabs.length : event.key === 'ArrowLeft' ? (index + ruleTabs.length - 1) % ruleTabs.length : event.key === 'Home' ? 0 : event.key === 'End' ? ruleTabs.length - 1 : undefined;
            if (next === undefined) return; event.preventDefault(); setActiveTab(ruleTabs[next].id); event.currentTarget.querySelector<HTMLButtonElement>(`#cloak-tab-${ruleTabs[next].id}`)?.focus();
          }}>{ruleTabs.map(tab => <button type="button" role="tab" tabIndex={activeTab === tab.id ? 0 : -1} id={`cloak-tab-${tab.id}`} aria-controls={`cloak-panel-${tab.id}`} aria-selected={activeTab === tab.id} key={tab.id} className={activeTab === tab.id ? 'active' : ''} onClick={() => setActiveTab(tab.id)}><tab.icon size={15} />{tab.label}{counts[tab.id] > 0 && <span>{counts[tab.id]}</span>}</button>)}</div>
          <div role="tabpanel" id={`cloak-panel-${activeTab}`} aria-labelledby={`cloak-tab-${activeTab}`} className="cloak-rule-panel">
            {activeTab === 'languages' && <><div className="cloak-rule-intro"><h3>拦截指定语言</h3><p>根据浏览器语言判断。选择“中文（全部地区）”时，也会匹配简体、繁体等中文变体。</p></div><ChoiceGrid label="语言" options={languageOptions} selected={draft.blocked_languages} onChange={values => update('blocked_languages', values)} /></>}
            {activeTab === 'ua' && <><div className="cloak-rule-intro"><h3>拦截指定 UA</h3><p>每行一个关键词，忽略大小写。访问者的 User-Agent 包含任意关键词时，进入安全页。</p></div><Field label="UA 拦截关键词" hint="最多 100 条，每条最多 200 个字符；按文本匹配，不使用正则表达式。"><textarea rows={8} placeholder={'例如：\ncurl\npython-requests\nHeadlessChrome'} maxLength={20100} value={uaText} onChange={event => { setUaText(event.target.value); update('blocked_uas', event.target.value.split(/\r?\n/).map(value => value.trim()).filter(Boolean)); }} /></Field><div className="cloak-rule-hint"><CircleHelp size={14} />留空即不启用 UA 拦截。</div></>}
            {activeTab === 'countries' && <><div className="cloak-rule-intro"><h3>拦截指定国家或地区</h3><p>勾选的国家和地区将进入安全页；可同时选择多个。无法定位的访问归入“未知地区”。</p></div>{!data.country_detection_configured && <div className="cloak-inline-message">地区识别暂未配置。可先保存规则，待管理员配置可信地区来源后生效。</div>}<ChoiceGrid label="国家或地区" options={countryOptions} selected={draft.blocked_countries} onChange={values => update('blocked_countries', values)} searchable /></>}
            {activeTab === 'platforms' && <><div className="cloak-rule-intro"><h3>仅允许选中的平台</h3><p>不选时不限制平台。选中后，访问链接须包含至少一个对应的非空点击参数，否则进入安全页。</p></div><div className="cloak-platform-grid" role="group" aria-label="允许平台">{platformOptions.map((platform, index) => <label key={platform.value} className={`cloak-platform-choice ${draft.allowed_platforms.includes(platform.value) ? 'selected' : ''}`}><input type="checkbox" checked={draft.allowed_platforms.includes(platform.value)} onChange={event => update('allowed_platforms', toggleValue(draft.allowed_platforms, platform.value, event.target.checked))} /><span className={`cloak-platform-mark platform-${platform.value}`}>{['T', 'f', 'G', 'B'][index]}</span><span><strong>{platform.label}</strong><small>点击参数 <code>{platform.param}</code></small></span><Check size={16} /></label>)}</div><div className="cloak-rule-hint"><CircleHelp size={14} />按链接参数判断平台，不代表对真实流量来源的认证。</div></>}
            {activeTab === 'devices' && <><div className="cloak-rule-intro"><h3>按设备与系统拦截</h3><p>先选择设备，再细分系统。勾选“全部系统”将拦截该设备的所有系统，包括无法识别的系统。</p></div><div className="cloak-device-grid">{deviceOptions.map(device => {
              const systems = draft.blocked_devices.find(rule => rule.device === device.value)?.systems || [];
              const all = systems.includes('all'); const Icon = deviceIcons[device.value];
              return <fieldset className={`cloak-device-card ${systems.length ? 'selected' : ''}`} key={device.value}><legend className="sr-only">{device.label}系统拦截</legend><label className="cloak-device-heading"><Icon size={22} /><strong>{device.label}</strong><input type="checkbox" aria-label={`拦截${device.label}`} checked={systems.length > 0} onChange={event => setDeviceSystems(device.value, event.target.checked ? ['all'] : [])} /></label><label className="cloak-system-all"><input type="checkbox" checked={all} onChange={event => changeSystem(device.value, 'all', event.target.checked)} /><span>全部系统</span></label><div className="cloak-system-options">{device.systems.map(system => <label key={system.value}><input type="checkbox" checked={all || systems.includes(system.value)} onChange={event => changeSystem(device.value, system.value, event.target.checked)} /><span>{system.label}</span></label>)}</div></fieldset>;
            })}</div><div className="cloak-rule-hint"><CircleHelp size={14} />设备与系统根据浏览器信息判断；未选中的设备和系统不拦截。</div></>}
          </div>
        </EditorSection>

        <EditorSection step="03" title="安全页" description="命中拦截条件，或未满足平台允许条件时，将进入此地址。"><Field label="安全页 URL" hint="填写完整的 HTTP / HTTPS 地址；访问参数不会传给安全页。"><input type="url" required maxLength={4096} placeholder="https://example.com/safe" value={draft.safe_url} onChange={event => update('safe_url', event.target.value)} /></Field></EditorSection>
      </div>

      <aside className="cloak-editor-aside"><div className="cloak-summary-card"><div className="cloak-summary-title"><ShieldCheck size={19} /><h2>访问路径</h2><span>实时预览</span></div><div className="cloak-route-start"><Link2 size={17} /><span>{editing ? editing.entry_url : '保存后生成专属斗篷入口'}</span></div><div className="cloak-route-connector"><ArrowDown size={15} /></div><div className="cloak-route-decision"><SlidersHorizontal size={18} /><strong>{draft.enabled ? '检查访问条件' : '拦截已关闭'}</strong><span>{draft.enabled ? `已设置 ${ruleCount(draft)} 类规则` : '所有访问直接进入目标链接'}</span></div><div className="cloak-route-branches"><div><span className="cloak-path-label">{draft.enabled ? '符合条件' : '直接访问'}</span><strong><ArrowRight size={14} />目标链接</strong><p>{target || '待选择目标链接'}</p></div><div><span className="cloak-path-label">{draft.enabled ? '命中拦截' : '目标不可用时'}</span><strong><ShieldCheck size={14} />安全页</strong><p>{draft.safe_url || '待填写安全页 URL'}</p></div></div><div className="cloak-summary-rules"><h3>已选条件</h3>{ruleTabs.map(tab => <button type="button" key={tab.id} onClick={() => setActiveTab(tab.id)}><span><tab.icon size={13} />{tab.label}</span><b>{counts[tab.id] ? `${counts[tab.id]} ${tab.id === 'devices' ? '类' : '项'}` : '未设置'}</b><ChevronRight size={12} /></button>)}</div><label className="cloak-enable-control"><input type="checkbox" checked={draft.enabled} onChange={event => update('enabled', event.target.checked)} /><span>启用拦截规则<small>关闭后直接访问目标链接</small></span></label></div><div className="cloak-editor-submit"><FormError>{message}</FormError><button className="btn primary full" disabled={busy}><Save size={16} />{busy ? '正在保存…' : editing ? '保存修改' : '保存并生成入口'}</button><button type="button" className="cloak-cancel" disabled={busy} onClick={() => { setDraft(undefined); setEditing(undefined); setMessage(''); }}>取消</button></div></aside>
      </form>
    </div>;
  }

  return <div className="cloak-page"><PageHeader eyebrow="CLOAK ROUTING" title="斗篷管理" subtitle="为每个入口设置访问条件，让不同访问进入对应页面。" action={<button className="btn primary" onClick={() => start()}><Plus size={16} />新建斗篷</button>} />
    {saved && <div className="cloak-saved"><span><Check size={18} /></span><div><strong>斗篷已保存，入口可以使用了</strong><p>{saved.entry_url}</p></div><CopyButton value={saved.entry_url} label="复制入口" /><button type="button" className="icon-btn" aria-label="关闭保存提示" onClick={() => setSaved(undefined)}><X size={16} /></button></div>}
    <div className="cloak-overview"><div className="cloak-overview-intro"><div><ShieldCheck size={25} strokeWidth={1.5} /></div><span><strong>一个入口，两条访问路径。</strong><small>符合条件进入目标链接，命中拦截进入安全页。</small></span></div><div className="cloak-overview-metrics"><div><strong>{data.data.length}</strong><span>全部斗篷</span></div><div><strong>{data.data.filter(item => item.enabled).length}</strong><span>开启拦截</span></div></div></div>
    {!deleting && <FormError>{message}</FormError>}
    <section className="panel cloak-list"><div className="panel-heading"><h2><ShieldCheck size={17} />我的斗篷</h2><span className="small muted">{data.data.length} 个入口</span></div>{!data.data.length ? <Empty title="让每一次访问，都有合适的去向" detail="连接分流或目标 URL，设置拦截条件，生成你的第一个斗篷入口。" action={<button className="btn secondary" onClick={() => start()}><Plus size={15} />创建第一个斗篷</button>} /> : <div className="table-wrap"><table><thead><tr><th>名称 / 斗篷入口</th><th>目标链接</th><th>访问规则</th><th>拦截状态</th><th className="right">操作</th></tr></thead><tbody>{data.data.map(item => <tr key={item.id}><td><div className="cloak-item-name"><span><ShieldCheck size={16} /></span><div><strong>{item.name}</strong><small>{date(item.updated_at)} 更新</small></div></div><div className="cloak-entry-link"><span>{item.entry_url}</span><CopyButton value={item.entry_url} /></div></td><td><span className="cloak-target-kind">{item.target_type === 'traffic_pool' ? '分流链接' : '自定义 URL'}</span><p className="cloak-table-url">{item.target_type === 'traffic_pool' ? trafficChoices.find(pool => pool.pool.slug === item.traffic_pool_slug)?.pool.name || item.traffic_pool_slug : item.target_url}</p></td><td><div className="cloak-rule-tags">{item.blocked_languages.length > 0 && <span>语言 {item.blocked_languages.length}</span>}{item.blocked_uas.length > 0 && <span>UA {item.blocked_uas.length}</span>}{item.blocked_countries.length > 0 && <span>国家 {item.blocked_countries.length}</span>}{item.allowed_platforms.length > 0 && <span>平台 {item.allowed_platforms.length}</span>}{item.blocked_devices.length > 0 && <span>设备 {item.blocked_devices.length}</span>}{!ruleCount(item) && <span className="cloak-no-rule">未设置条件</span>}</div></td><td><Badge tone={item.enabled ? 'blue' : 'gray'}>{item.enabled ? '已开启' : '已关闭'}</Badge></td><td><div className="row-actions wrap"><button className="text-btn" onClick={() => start(item)}>编辑</button><button className="text-btn" disabled={busy} onClick={() => toggle(item)}>{item.enabled ? '关闭拦截' : '开启拦截'}</button><button className="text-btn danger-text" disabled={busy} onClick={() => { setMessage(''); setDeleting(item); }}>删除</button></div></td></tr>)}</tbody></table></div>}</section>
    <p className="cloak-bottom-note"><ShieldOff size={14} />未选择的拦截条件不生效；关闭拦截规则后，入口直接进入目标链接。</p>
    {deleting && <Modal title="删除斗篷？" close={() => { if (!busy) setDeleting(undefined); }}><p>删除「{deleting.name}」后，斗篷入口将停止访问。</p><FormError>{message}</FormError><div className="modal-footer"><button className="btn secondary" disabled={busy} onClick={() => setDeleting(undefined)}>取消</button><button className="btn danger" disabled={busy} onClick={remove}>删除斗篷</button></div></Modal>}
  </div>;
}
