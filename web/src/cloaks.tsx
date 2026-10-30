import { VisitStatisticsButton } from './visit-statistics';
// i18n: static UI literals
import { localizedCountry, localizedLanguage, t, translateContent, useLocale } from './i18n';
import { useState, type FormEvent, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { Link } from 'react-router-dom';
import { ArrowDown, ArrowLeft, ArrowRight, Check, ChevronRight, CircleHelp, Copy, Globe2, Languages, Link2, ListFilter, Monitor, Pencil, Plus, Save, Search, ShieldCheck, ShieldOff, SlidersHorizontal, Smartphone, Tablet, Trash2, X } from 'lucide-react';
import { api, send } from './api';
import { date, Empty, ErrorState, errorText, Field, FormError, Modal, PageHeader, Spinner } from './ui';
import { DomainShareDrawer } from './domain-share-drawer';
import { countryOptions as originalCountries, deviceOptions as originalDevices, languageOptions as originalLanguages, platformOptions, type CloakOption } from './cloak-options';
import './cloaks.css';

export type CloakStatus = 'rules' | 'off' | 'blocked';
type Device = 'desktop' | 'tablet' | 'mobile';
type DeviceRule = { device: Device; systems: string[] };
export type CloakDraft = {
  name: string; status: CloakStatus; enabled: boolean; target_type: 'url' | 'traffic_pool' | 'app' | 'pwa'; target_url: string; traffic_pool_slug: string; target_id: string; app_target_mode: '' | 'direct' | 'info'; safe_url: string;
  allowed_ips: string[]; blocked_ips: string[]; blocked_languages: string[]; block_empty_language: boolean; blocked_browsers: string[]; blocked_uas: string[]; allowed_countries: string[]; blocked_countries: string[]; allowed_platforms: string[]; blocked_devices: DeviceRule[];
};
export type Cloak = CloakDraft & { id: string; slug: string; entry_url: string; created_at: string; updated_at: string };
type CloaksResponse = { data: Cloak[]; country_detection_configured: boolean };
type TrafficChoice = { pool: { slug: string; name: string; enabled: boolean }; entry_url: string };
type AppChoice = { id: string; name: string; direct_url?: string; detail_url?: string; entry_url?: string };
type TargetChoices = { apps: AppChoice[]; pwas: AppChoice[] };
type RuleTab = 'ips' | 'languages' | 'browsers' | 'ua' | 'countries' | 'platforms' | 'devices';
const ruleTabs: Array<{ id: RuleTab; label: string; icon: typeof Languages }> = [
  { id: 'ips', get label() { return t("IP 名单"); }, icon: ShieldCheck },
  { id: 'languages', get label() { return t("语言拦截"); }, icon: Languages }, { id: 'browsers', get label() { return t("客户端"); }, icon: Globe2 }, { id: 'ua', get label() { return t("UA 拦截"); }, icon: ListFilter }, { id: 'countries', get label() { return t("允许国家"); }, icon: Globe2 }, { id: 'platforms', get label() { return t("广告允许"); }, icon: Link2 }, { id: 'devices', get label() { return t("设备与系统"); }, icon: Monitor },
];
const browserOptions: CloakOption[] = [{ value: 'chrome', label: 'Chrome' }, { value: 'safari', label: 'Safari' }, { value: 'firefox', label: 'Firefox' }, { value: 'edge', label: 'Edge' }];
const newDraft = (): CloakDraft => ({ name: '', status: 'rules', enabled: true, target_type: 'traffic_pool', target_url: '', traffic_pool_slug: '', target_id: '', app_target_mode: '', safe_url: '', allowed_ips: [], blocked_ips: [], blocked_languages: [], block_empty_language: false, blocked_browsers: [], blocked_uas: [], allowed_countries: [], blocked_countries: [], allowed_platforms: [], blocked_devices: [] });
const statusOf = (item: { status?: CloakStatus; enabled: boolean }): CloakStatus => item.status || (item.enabled ? 'rules' : 'off');
const statusLabel = (status: CloakStatus) => status === 'rules' ? t("斗篷状态") : status === 'off' ? t("关闭状态") : t("屏蔽状态");
const statusHelp = (status: CloakStatus) => status === 'rules' ? t("按 IP 黑名单、白名单和其他规则判断访问。") : status === 'off' ? t("跳过全部斗篷规则，包括 IP 黑名单，访问目标链接。") : t("全部访问进入安全页，包括 IP 白名单。");
const editable = (item: CloakDraft): CloakDraft => ({ name: item.name, status: statusOf(item), enabled: statusOf(item) === 'rules', target_type: item.target_type, target_url: item.target_url, traffic_pool_slug: item.traffic_pool_slug, target_id: item.target_id || '', app_target_mode: item.app_target_mode || '', safe_url: item.safe_url, allowed_ips: item.allowed_ips || [], blocked_ips: item.blocked_ips || [], blocked_languages: item.blocked_languages, block_empty_language: item.block_empty_language || false, blocked_browsers: item.blocked_browsers || [], blocked_uas: item.blocked_uas, allowed_countries: item.allowed_countries || [], blocked_countries: item.blocked_countries || [], allowed_platforms: item.allowed_platforms, blocked_devices: item.blocked_devices });
const toggleValue = (values: string[], value: string, selected: boolean) => selected ? [...values.filter(item => item.toLowerCase() !== value.toLowerCase()), value] : values.filter(item => item.toLowerCase() !== value.toLowerCase());
const labelFor = (options: CloakOption[], value: string) => options.find(option => option.value.toLowerCase() === value.toLowerCase())?.label || value;
const ruleCount = (item: CloakDraft) => [(item.allowed_ips?.length || 0) + (item.blocked_ips?.length || 0), item.blocked_languages.length || item.block_empty_language, item.blocked_browsers?.length, item.blocked_uas.length, (item.allowed_countries?.length || item.blocked_countries?.length || 0), item.allowed_platforms.length, item.blocked_devices.length].filter(Boolean).length;
const ipLines = (text: string) => text.split(/\r?\n/).map(value => value.trim()).filter(Boolean);
const deviceIcons = { desktop: Monitor, tablet: Tablet, mobile: Smartphone };

function ChoiceGrid({ label, options, selected, onChange, searchable = false, showCodes = true }: { label: string; options: CloakOption[]; selected: string[]; onChange: (values: string[]) => void; searchable?: boolean; showCodes?: boolean }) {
  useLocale();
  const [search, setSearch] = useState('');
  const visible = options.filter(option => `${option.label} ${option.value}`.toLowerCase().includes(search.trim().toLowerCase()));
  return <div className="cloak-choice-picker">
    <div className="cloak-picker-toolbar">{searchable ? <label className="cloak-search"><Search size={15} /><input aria-label={t("搜索{0}", { 0: label })} placeholder={t("搜索{0}或代码", { 0: label })} value={search} onChange={event => setSearch(event.target.value)} /></label> : <span>{t("勾选需要拦截的")}{label}</span>}<button type="button" className="text-btn" disabled={!selected.length} onClick={() => onChange([])}>{t("清空选择")}</button></div>
    {!!selected.length && <div className="cloak-selected-values" aria-label={t("已选{0}", { 0: label })}>{selected.map(value => <button type="button" key={value} onClick={() => onChange(selected.filter(item => item !== value))} aria-label={t("移除{0}", { 0: labelFor(options, value) })}>{labelFor(options, value)}<X size={12} /></button>)}</div>}
    <div className="cloak-choice-grid" role="group" aria-label={label}>{visible.map(option => { const checked = selected.some(value => value.toLowerCase() === option.value.toLowerCase()); return <label className={`cloak-choice ${checked ? 'selected' : ''}`} key={option.value}><input type="checkbox" checked={checked} onChange={event => onChange(toggleValue(selected, option.value, event.target.checked))} /><span>{option.label}{showCodes && <small>{option.value}</small>}</span></label>; })}{!visible.length && <p className="cloak-no-results">{t("没有找到匹配的")}{label}{t("，试试名称或代码。")}</p>}</div>
  </div>;
}

function StatusControl({ value, onChange, label, disabled = false, compact = false }: { value: CloakStatus; onChange: (status: CloakStatus) => void; label: string; disabled?: boolean; compact?: boolean }) {
  useLocale();
  return <div className={`cloak-status-segments${compact ? ' compact' : ''}`} role="group" aria-label={label}>{(['rules', 'off', 'blocked'] as const).map(status => <button type="button" key={status} className={`cloak-status-option status-${status}`} aria-pressed={value === status} title={statusHelp(status)} disabled={disabled} onClick={() => { if (value !== status) onChange(status); }}><span className="cloak-status-dot" aria-hidden="true" /><span>{statusLabel(status)}</span></button>)}</div>;
}

function StatusDialogButton({ item, disabled, onChange }: { item: Cloak; disabled: boolean; onChange: (status: CloakStatus) => Promise<void> }) {
  useLocale();
  const [open, setOpen] = useState(false);
  const [selected, setSelected] = useState(statusOf(item));
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');
  const current = statusOf(item);
  const close = () => { if (!saving) setOpen(false); };
  const confirm = async () => {
    if (selected === current) { close(); return; }
    setSaving(true); setError('');
    try { await onChange(selected); setOpen(false); }
    catch (error) { setError(errorText(error)); }
    finally { setSaving(false); }
  };
  return <><button type="button" className="text-btn cloak-status-trigger" aria-label={t("斗篷状态：{0}", { 0: item.name })} aria-haspopup="dialog" disabled={disabled} onClick={() => { setSelected(current); setError(''); setOpen(true); }}><SlidersHorizontal size={14} />{t("斗篷状态")}</button>{open && <Modal title={t("斗篷状态")} close={close}><div className="cloak-status-dialog"><p className="cloak-status-dialog-name">{item.name}</p><div className="cloak-status-cards" role="radiogroup" aria-label={t("斗篷状态")}>{(['rules', 'off', 'blocked'] as const).map(status => <label key={status} className={`cloak-status-card status-${status}${selected === status ? ' selected' : ''}`}><input type="radio" name={`cloak-status-${item.id}`} value={status} checked={selected === status} disabled={saving} onChange={() => setSelected(status)} /><span><strong>{statusLabel(status)}</strong><small>{statusHelp(status)}</small>{status === 'rules' && <small>{t("符合条件进入目标链接，命中拦截进入安全页。")}</small>}</span></label>)}</div><FormError>{error}</FormError><div className="modal-footer"><button type="button" className="btn secondary" disabled={saving} onClick={close}>{t("取消")}</button><button type="button" className="btn primary" disabled={saving || selected === current} onClick={confirm}>{saving ? t("正在保存…") : t("确认")}</button></div></div></Modal>}</>;
}

function EditorSection({ step, title, description, children }: { step: string; title: string; description: string; children: ReactNode }) {
  useLocale();
  return <section className="cloak-editor-section"><header><span className="cloak-step-number">{step}</span><div><h2>{title}</h2><p>{description}</p></div></header>{children}</section>;
}

export function CloaksPage() {
  useLocale();
  const countryOptions = originalCountries.map(option => ({ ...option, label: localizedCountry(option.value, option.label) }));
  const languageOptions = originalLanguages.map(option => ({ ...option, label: localizedLanguage(option.value, option.label) }));
  const deviceOptions = translateContent(originalDevices);
  const { data, error, refetch } = useQuery({ queryKey: ['cloaks'], queryFn: () => api<CloaksResponse>('/cloaks') });
  const pools = useQuery({ queryKey: ['traffic-pools'], queryFn: () => api<{ data: TrafficChoice[] }>('/traffic-pools') });
  const targets = useQuery({ queryKey: ['cloak-targets'], queryFn: () => api<TargetChoices>('/cloak-targets') });
  const [draft, setDraft] = useState<CloakDraft>();
  const [editing, setEditing] = useState<Cloak>();
  const [deleting, setDeleting] = useState<Cloak>();
  const [activeTab, setActiveTab] = useState<RuleTab>('languages');
  const [uaText, setUaText] = useState('');
  const [allowedIPText, setAllowedIPText] = useState('');
  const [blockedIPText, setBlockedIPText] = useState('');
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState('');
  const [saved, setSaved] = useState<Cloak>();
  const [sharing, setSharing] = useState<Cloak>();
  const start = (item?: Cloak) => {
    setEditing(item); setDraft(item ? editable(item) : newDraft());
    setAllowedIPText(item?.allowed_ips?.join('\n') || ''); setBlockedIPText(item?.blocked_ips?.join('\n') || '');
    setUaText(item?.blocked_uas.join('\n') || ''); setActiveTab('languages'); setMessage(''); setSaved(undefined);
  };
  const update = <K extends keyof CloakDraft>(key: K, value: CloakDraft[K]) => setDraft(current => current ? { ...current, [key]: value } : current);
  const save = async (event: FormEvent) => {
    event.preventDefault(); if (!draft) return; setBusy(true); setMessage('');
    try {
      const payload: CloakDraft = { ...editable(draft), name: draft.name.trim(), target_url: draft.target_type === 'url' ? draft.target_url.trim() : '', traffic_pool_slug: draft.target_type === 'traffic_pool' ? draft.traffic_pool_slug : '', target_id: draft.target_type === 'app' || draft.target_type === 'pwa' ? draft.target_id : '', app_target_mode: draft.target_type === 'app' ? draft.app_target_mode || 'direct' : '', safe_url: draft.safe_url.trim(), allowed_ips: ipLines(allowedIPText), blocked_ips: ipLines(blockedIPText), blocked_uas: uaText.split(/\r?\n/).map(value => value.trim()).filter(Boolean) };
      const result = await send<Cloak>(editing ? `/cloaks/${editing.id}` : '/cloaks', payload, editing ? 'PUT' : 'POST');
      setSaved(result); setDraft(undefined); setEditing(undefined); await refetch();
    } catch (error) { setMessage(errorText(error)); } finally { setBusy(false); }
  };
  const changeStatus = async (item: Cloak, status: CloakStatus) => {
    setBusy(true); setMessage(''); setSaved(undefined);
    try { await send(`/cloaks/${item.id}`, { ...editable(item), status, enabled: status === 'rules' }, 'PUT'); await refetch(); }
    finally { setBusy(false); }
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
  const appChoices = targets.data?.apps || [];
  const pwaChoices = targets.data?.pwas || [];
  const targetName = (item: CloakDraft) => item.target_type === 'traffic_pool' ? trafficChoices.find(pool => pool.pool.slug === item.traffic_pool_slug)?.pool.name || item.traffic_pool_slug : item.target_type === 'url' ? item.target_url : (item.target_type === 'pwa' ? pwaChoices : appChoices).find(app => app.id === item.target_id)?.name || t("目标不可用");
  const targetKind = (item: CloakDraft) => item.target_type === 'traffic_pool' ? t("分流链接") : item.target_type === 'pwa' ? t("PWA 应用") : item.target_type === 'app' ? item.app_target_mode === 'info' ? t("详情页") : t("直接下载") : t("自定义 URL");

  if (draft) {
    const counts: Record<RuleTab, number> = { ips: ipLines(allowedIPText).length + ipLines(blockedIPText).length, languages: draft.blocked_languages.length + Number(draft.block_empty_language), browsers: draft.blocked_browsers.length, ua: uaText.split(/\r?\n/).filter(value => value.trim()).length, countries: draft.allowed_countries.length || draft.blocked_countries.length, platforms: draft.allowed_platforms.length, devices: draft.blocked_devices.length };
    const chosenApp = (draft.target_type === 'pwa' ? pwaChoices : appChoices).find(app => app.id === draft.target_id);
    const target = draft.target_type === 'url' ? draft.target_url : draft.target_type === 'traffic_pool' ? trafficChoices.find(item => item.pool.slug === draft.traffic_pool_slug)?.entry_url : draft.target_type === 'pwa' ? chosenApp?.entry_url : draft.app_target_mode === 'info' ? chosenApp?.detail_url : chosenApp?.direct_url;
    const safeURL = draft.safe_url.trim();
    const status = statusOf(draft);
    return <div className="cloak-page">
      <div className="cloak-editor-back"><button type="button" onClick={() => { setDraft(undefined); setEditing(undefined); setMessage(''); }} disabled={busy}><ArrowLeft size={15} />{t("返回斗篷列表")}</button><span>{t("访问规则工作台")}</span></div>
      <PageHeader eyebrow="CLOAK ROUTING" title={editing ? t("编辑斗篷") : t("新建斗篷")} subtitle={t("先选择目标，再设置访问条件；安全页可自定义，也可使用默认 404 页面。")} />
      <form onSubmit={save} className="cloak-editor-layout"><div className="cloak-editor-main">
        <EditorSection step="01" title={t("目标链接")} description={t("未命中拦截条件的访问，将进入这里。")}>
          <Field label={t("斗篷名称")}><input required maxLength={60} placeholder={t("例如：海外应用推广")} value={draft.name} onChange={event => update('name', event.target.value)} /></Field>
          <div className="cloak-target-types" role="group" aria-label={t("目标类型")}>
            <label className={draft.target_type === 'traffic_pool' ? 'selected' : ''}><input type="radio" name="cloak-target-type" value="traffic_pool" checked={draft.target_type === 'traffic_pool'} onChange={() => update('target_type', 'traffic_pool')} /><span className="cloak-type-icon"><SlidersHorizontal size={20} /></span><span><strong>{t("选择分流链接")}</strong><small>{t("沿用已有分流的分配规则")}</small></span><span className="cloak-radio-dot" /></label>
            <label className={draft.target_type === 'pwa' ? 'selected' : ''}><input type="radio" name="cloak-target-type" value="pwa" checked={draft.target_type === 'pwa'} onChange={() => setDraft({ ...draft, target_type: 'pwa', target_id: draft.target_type === 'pwa' ? draft.target_id : '' })} /><span className="cloak-type-icon"><Globe2 size={20} /></span><span><strong>{t("PWA 应用")}</strong><small>{t("安装链接")}</small></span><span className="cloak-radio-dot" /></label>
            <label className={draft.target_type === 'app' ? 'selected' : ''}><input type="radio" name="cloak-target-type" value="app" checked={draft.target_type === 'app'} onChange={() => setDraft({ ...draft, target_type: 'app', target_id: draft.target_type === 'app' ? draft.target_id : '', app_target_mode: draft.app_target_mode || 'direct' })} /><span className="cloak-type-icon"><Smartphone size={20} /></span><span><strong>{t("应用链接")}</strong><small>{t("直接下载 / 详情页")}</small></span><span className="cloak-radio-dot" /></label>
          </div>
          {draft.target_type === 'url' ? <div className="cloak-legacy-target"><p>{t("已保留原目标链接，可切换为分流、PWA 或应用。")}</p><code>{draft.target_url}</code></div> : draft.target_type === 'traffic_pool' ? <div><Field label={t("分流链接")}><select required value={draft.traffic_pool_slug} onChange={event => update('traffic_pool_slug', event.target.value)}><option value="">{pools.isPending ? t("正在读取分流…") : t("请选择分流链接")}</option>{trafficChoices.map(item => <option key={item.pool.slug} value={item.pool.slug}>{item.pool.name || item.pool.slug}{!item.pool.enabled ? t("（已关闭）") : ''}</option>)}</select></Field>{pools.error ? <div className="cloak-inline-message">{t("分流读取失败。")}<button type="button" className="text-btn" onClick={() => pools.refetch()}>{t("重试")}</button></div> : !pools.isPending && !trafficChoices.length ? <p className="cloak-field-help">{t("还没有分流链接。可以")}<Link to="/traffic">{t("创建分流")}</Link></p> : null}</div> : <div>
            <Field label={draft.target_type === 'pwa' ? t("PWA 应用") : t("应用链接")}><select required value={draft.target_id} onChange={event => update('target_id', event.target.value)} disabled={targets.isPending || !!targets.error}><option value="">{targets.isPending ? t("加载中…") : t("请选择应用")}</option>{draft.target_id && !chosenApp && <option value={draft.target_id} disabled>{t("目标不可用")}</option>}{(draft.target_type === 'pwa' ? pwaChoices : appChoices).map(app => <option key={app.id} value={app.id}>{app.name}</option>)}</select></Field>
            {draft.target_type === 'app' && <div className="cloak-app-modes" role="group" aria-label={t("跳转方式")}>{(['direct', 'info'] as const).map(mode => <button type="button" key={mode} className={draft.app_target_mode === mode ? 'selected' : ''} aria-pressed={draft.app_target_mode === mode} onClick={() => update('app_target_mode', mode)}>{mode === 'direct' ? t("直接下载") : t("详情页")}</button>)}</div>}
            {targets.error ? <div className="cloak-inline-message">{t("加载失败")}<button type="button" className="text-btn" onClick={() => targets.refetch()}>{t("重试")}</button></div> : !targets.isPending && !(draft.target_type === 'pwa' ? pwaChoices : appChoices).length && <p className="cloak-field-help">{t("暂无可用应用")}</p>}
          </div>}
          {draft.target_type !== 'url' && target && <div className="cloak-target-preview"><Link2 size={14} /><span>{target}</span></div>}
        </EditorSection>

        <EditorSection step="02" title={t("访问规则")} description={t("按规则状态下，先检查 IP 黑名单，再检查白名单；其余访问按语言、客户端等条件判断。")}>
          <div className="cloak-rule-tabs" role="tablist" aria-label={t("访问规则")} onKeyDown={event => {
            const index = ruleTabs.findIndex(tab => tab.id === activeTab);
            const next = event.key === 'ArrowRight' ? (index + 1) % ruleTabs.length : event.key === 'ArrowLeft' ? (index + ruleTabs.length - 1) % ruleTabs.length : event.key === 'Home' ? 0 : event.key === 'End' ? ruleTabs.length - 1 : undefined;
            if (next === undefined) return; event.preventDefault(); setActiveTab(ruleTabs[next].id); event.currentTarget.querySelector<HTMLButtonElement>(`#cloak-tab-${ruleTabs[next].id}`)?.focus();
          }}>{ruleTabs.map(tab => <button type="button" role="tab" tabIndex={activeTab === tab.id ? 0 : -1} id={`cloak-tab-${tab.id}`} aria-controls={`cloak-panel-${tab.id}`} aria-selected={activeTab === tab.id} key={tab.id} className={activeTab === tab.id ? 'active' : ''} onClick={() => setActiveTab(tab.id)}><tab.icon size={15} />{tab.label}{counts[tab.id] > 0 && <span>{counts[tab.id]}</span>}</button>)}</div>
          <div role="tabpanel" id={`cloak-panel-${activeTab}`} aria-labelledby={`cloak-tab-${activeTab}`} className="cloak-rule-panel">
            {activeTab === 'ips' && <><div className="cloak-rule-intro"><h3>{t("IP 白名单与黑名单")}</h3><p>{t("按规则状态下，黑名单直接进入安全页，白名单跳过斗篷的其他拦截规则。同一 IP 同时在两份名单中时，黑名单优先。")}</p></div><div className="cloak-ip-lists">{(['allowed', 'blocked'] as const).map(kind => { const allowed = kind === 'allowed'; const value = allowed ? allowedIPText : blockedIPText; const label = allowed ? t("IP 白名单") : t("IP 黑名单"); return <div key={kind}><Field label={label} hint={t("每行一个 IPv4 或 IPv6 地址，每份名单最多 1000 项，不支持 CIDR。") }><textarea aria-label={label} rows={7} placeholder={"192.0.2.1\n2001:db8::1"} value={value} onChange={event => { const text = event.target.value; if (allowed) setAllowedIPText(text); else setBlockedIPText(text); update(allowed ? 'allowed_ips' : 'blocked_ips', ipLines(text)); }} /></Field><button type="button" className="text-btn" disabled={!value} onClick={() => { if (allowed) setAllowedIPText(''); else setBlockedIPText(''); update(allowed ? 'allowed_ips' : 'blocked_ips', []); }}>{allowed ? t("清空白名单") : t("清空黑名单")}</button></div>; })}</div><div className="cloak-rule-hint"><CircleHelp size={14} />{t("未列入名单的 IP 继续按原规则判断；白名单不绕过账号或下载访问限制。")}</div></>}
            {activeTab === 'languages' && <><div className="cloak-rule-intro"><h3>{t("拦截指定语言")}</h3><p>{t("根据浏览器语言判断。选择“中文（全部地区）”时，也会匹配简体、繁体等中文变体。")}</p></div><label className="cloak-empty-language"><span><strong>{t("屏蔽空语言访问")}</strong><small id="cloak-empty-language-help">{t("开启后，Accept-Language 缺失或没有有效语言（q > 0）的访问将进入安全页。")}</small></span><input type="checkbox" role="switch" aria-label={t("屏蔽空语言访问")} aria-describedby="cloak-empty-language-help" checked={draft.block_empty_language} onChange={event => update('block_empty_language', event.target.checked)} /></label><ChoiceGrid label={t("语言")} options={languageOptions} selected={draft.blocked_languages} onChange={values => update('blocked_languages', values)} /></>}
            {activeTab === 'browsers' && <><div className="cloak-rule-intro"><h3>{t("拦截指定客户端")}</h3><p>{t("根据浏览器 User-Agent 判断客户端；勾选的浏览器访问将进入安全页。")}</p></div><div className="cloak-browser-options"><ChoiceGrid label={t("客户端")} options={browserOptions} selected={draft.blocked_browsers} onChange={values => update('blocked_browsers', values)} showCodes={false} /></div></>}
            {activeTab === 'ua' && <><div className="cloak-rule-intro"><h3>{t("拦截指定 UA")}</h3><p>{t("每行一个关键词，忽略大小写。访问者的 User-Agent 包含任意关键词时，进入安全页。")}</p></div><Field label={t("UA 拦截关键词")} hint={t("最多 100 条，每条最多 200 个字符；按文本匹配，不使用正则表达式。")}><textarea rows={8} placeholder={t("例如：\ncurl\npython-requests\nHeadlessChrome")} maxLength={20100} value={uaText} onChange={event => { setUaText(event.target.value); update('blocked_uas', event.target.value.split(/\r?\n/).map(value => value.trim()).filter(Boolean)); }} /></Field><div className="cloak-rule-hint"><CircleHelp size={14} />{t("留空即不启用 UA 拦截。")}</div></>}
            {activeTab === 'countries' && <><div className="cloak-rule-intro"><h3>{t("勾选允许国家或地区")}</h3><p>{t("勾选后，仅所选国家和地区可通过；不选则不限制国家。无法定位的访问按“未知地区”判断。")}</p></div>{!data.country_detection_configured && <div className="cloak-inline-message">{t("地区识别暂未配置，访问将按“未知地区”判断。")}</div>}{draft.blocked_countries.length > 0 && <div className="cloak-inline-message">{t("此入口保留原有国家拦截规则；更改允许国家后，原规则将被替换。")}<button type="button" className="text-btn" onClick={() => setDraft({ ...draft, blocked_countries: [] })}>{t("清除原国家拦截规则")}</button></div>}<ChoiceGrid label={t("国家或地区")} options={countryOptions} selected={draft.allowed_countries} onChange={values => setDraft({ ...draft, allowed_countries: values, blocked_countries: [] })} searchable /></>}
            {activeTab === 'platforms' && <><div className="cloak-rule-intro"><h3>{t("仅允许选中的广告平台")}</h3><p>{t("选择允许的广告平台，可多选；不选则不限制。")}</p></div><div className="cloak-platform-grid" role="group" aria-label={t("允许广告平台")}>{platformOptions.map((platform, index) => <label key={platform.value} className={`cloak-platform-choice ${draft.allowed_platforms.includes(platform.value) ? 'selected' : ''}`}><input type="checkbox" checked={draft.allowed_platforms.includes(platform.value)} onChange={event => update('allowed_platforms', toggleValue(draft.allowed_platforms, platform.value, event.target.checked))} /><span className={`cloak-platform-mark platform-${platform.value}`}>{['T', 'f', 'G', 'B'][index]}</span><span><strong>{platform.label}</strong></span><Check size={16} /></label>)}</div></>}
            {activeTab === 'devices' && <><div className="cloak-rule-intro"><h3>{t("按设备与系统拦截")}</h3><p>{t("先选择设备，再细分系统。勾选“全部系统”将拦截该设备的所有系统，包括无法识别的系统。")}</p></div><div className="cloak-device-grid">{deviceOptions.map(device => {
              const systems = draft.blocked_devices.find(rule => rule.device === device.value)?.systems || [];
              const all = systems.includes('all'); const Icon = deviceIcons[device.value];
              return <fieldset className={`cloak-device-card ${systems.length ? 'selected' : ''}`} key={device.value}><legend className="sr-only">{device.label}{t("系统拦截")}</legend><label className="cloak-device-heading"><Icon size={22} /><strong>{device.label}</strong><input type="checkbox" aria-label={t("拦截{0}", { 0: device.label })} checked={systems.length > 0} onChange={event => setDeviceSystems(device.value, event.target.checked ? ['all'] : [])} /></label><label className="cloak-system-all"><input type="checkbox" checked={all} onChange={event => changeSystem(device.value, 'all', event.target.checked)} /><span>{t("全部系统")}</span></label><div className="cloak-system-options">{device.systems.map(system => <label key={system.value}><input type="checkbox" checked={all || systems.includes(system.value)} onChange={event => changeSystem(device.value, system.value, event.target.checked)} /><span>{system.label}</span></label>)}</div></fieldset>;
            })}</div><div className="cloak-rule-hint"><CircleHelp size={14} />{t("设备与系统根据浏览器信息判断；未选中的设备和系统不拦截。")}</div></>}
          </div>
        </EditorSection>

        <EditorSection step="03" title={t("安全页")} description={t("命中拦截条件、未满足国家或广告允许条件或目标不可用时，进入自定义安全页；留空则显示默认英文 404 页面。")}>
          <Field label={t("安全页 URL（选填）")}><input type="url" maxLength={4096} placeholder={t("留空使用默认 404 页面")} value={draft.safe_url} onChange={event => update('safe_url', event.target.value)} /></Field>
        </EditorSection>
      </div>

      <aside className="cloak-editor-aside"><div className="cloak-summary-card"><div className="cloak-summary-title"><ShieldCheck size={19} /><h2>{t("访问路径")}</h2><span>{t("实时预览")}</span></div><div className="cloak-route-start"><Link2 size={17} /><span>{editing ? editing.entry_url : t("保存后生成专属斗篷入口")}</span></div><div className="cloak-route-connector"><ArrowDown size={15} /></div><div className="cloak-route-decision"><SlidersHorizontal size={18} /><strong>{statusLabel(status)}</strong><span>{status === 'rules' ? t("已设置 {0} 类规则", { 0: ruleCount(draft) }) : statusHelp(status)}</span></div><div className={`cloak-route-branches ${status === 'blocked' ? 'single-path' : ''}`}>{status !== 'blocked' && <div><span className="cloak-path-label">{status === 'rules' ? t("符合条件") : t("直接访问")}</span><strong><ArrowRight size={14} />{t("目标链接")}</strong><p>{target || t("待选择目标链接")}</p></div>}<div><span className="cloak-path-label">{status === 'blocked' ? t("全部访问") : status === 'rules' ? t("命中拦截") : t("目标不可用时")}</span><strong><ShieldCheck size={14} />{safeURL ? t("自定义安全页") : t("默认 404 页面")}</strong><p>{safeURL || 'Account not found'}</p></div></div><div className="cloak-summary-rules"><h3>{t("已选条件")}</h3>{ruleTabs.map(tab => <button type="button" key={tab.id} onClick={() => setActiveTab(tab.id)}><span><tab.icon size={13} />{tab.label}</span><b>{counts[tab.id] ? `${counts[tab.id]} ${tab.id === 'devices' ? t("类") : t("项")}` : t("未设置")}</b><ChevronRight size={12} /></button>)}</div><div className="cloak-status-control"><span>{t("斗篷状态")}</span><StatusControl label={t("斗篷状态")} value={status} disabled={busy} onChange={next => setDraft({ ...draft, status: next, enabled: next === 'rules' })} /><small>{statusHelp(status)}</small></div></div><div className="cloak-editor-submit"><FormError>{message}</FormError><button className="btn primary full" disabled={busy}><Save size={16} />{busy ? t("正在保存…") : editing ? t("保存修改") : t("保存并生成入口")}</button><button type="button" className="cloak-cancel" disabled={busy} onClick={() => { setDraft(undefined); setEditing(undefined); setMessage(''); }}>{t("取消")}</button></div></aside>
      </form>
    </div>;
  }

  return <div className="cloak-page"><PageHeader eyebrow="CLOAK ROUTING" title={t("斗篷管理")} subtitle={t("为每个入口设置访问条件，让不同访问进入对应页面。")} action={<button className="btn primary" onClick={() => start()}><Plus size={16} />{t("新建斗篷")}</button>} />
    {saved && <div className="cloak-saved"><span><Check size={18} /></span><div><strong>{t("斗篷已保存，入口可以使用了")}</strong><p>{saved.entry_url}</p></div><button type="button" className="text-btn" onClick={() => setSharing(saved)}><Copy size={14} />{t("复制入口")}</button><button type="button" className="icon-btn" aria-label={t("关闭保存提示")} onClick={() => setSaved(undefined)}><X size={16} /></button></div>}
    <div className="cloak-overview"><div className="cloak-overview-intro"><div><ShieldCheck size={25} strokeWidth={1.5} /></div><span><strong>{t("一个入口，两条访问路径。")}</strong><small>{t("符合条件进入目标链接，命中拦截进入安全页。")}</small></span></div><div className="cloak-overview-metrics"><div><strong>{data.data.length}</strong><span>{t("全部斗篷")}</span></div><div><strong>{data.data.filter(item => statusOf(item) === 'rules').length}</strong><span>{t("按规则")}</span></div></div></div>
    {!deleting && <FormError>{message}</FormError>}
    <section className="panel cloak-list"><div className="panel-heading"><h2><ShieldCheck size={17} />{t("我的斗篷")}</h2><span className="small muted">{data.data.length} {t(" 个入口")}</span></div>{!data.data.length ? <Empty title={t("让每一次访问，都有合适的去向")} detail={t("连接分流、PWA 或应用，设置访问条件并生成入口。")} action={<button className="btn secondary" onClick={() => start()}><Plus size={15} />{t("创建第一个斗篷")}</button>} /> : <div className="table-wrap"><table><thead><tr><th>{t("名称 / 斗篷入口")}</th><th>{t("目标链接")}</th><th>{t("访问规则")}</th><th>{t("斗篷状态")}</th><th className="right">{t("操作")}</th></tr></thead><tbody>{data.data.map(item => <tr key={item.id}><td><div className="cloak-item-name"><span><ShieldCheck size={16} /></span><div><strong>{item.name}</strong><small>{date(item.updated_at)} {t(" 更新")}</small></div></div><div className="cloak-entry-link"><span>{item.entry_url}</span><button type="button" className="text-btn" aria-label={t("复制入口")} onClick={() => setSharing(item)}><Copy size={14} />{t("复制入口")}</button></div></td><td><span className="cloak-target-kind">{targetKind(item)}</span><p className="cloak-table-url">{targetName(item)}</p></td><td><div className="cloak-rule-tags">{!!item.allowed_ips?.length && <span>{t("白名单 ")}{item.allowed_ips.length}</span>}{!!item.blocked_ips?.length && <span>{t("黑名单 ")}{item.blocked_ips.length}</span>}{item.blocked_languages.length > 0 && <span>{t("语言 ")}{item.blocked_languages.length}</span>}{item.block_empty_language && <span>{t("空语言")}</span>}{!!item.blocked_browsers?.length && <span>{t("客户端 ")}{item.blocked_browsers.length}</span>}{item.blocked_uas.length > 0 && <span>UA {item.blocked_uas.length}</span>}{!!item.allowed_countries?.length && <span>{t("允许国家")}{" "}{item.allowed_countries.length}</span>}{!!item.blocked_countries?.length && <span>{t("国家 ")}{item.blocked_countries.length}</span>}{item.allowed_platforms.length > 0 && <span>{t("广告 ")}{item.allowed_platforms.length}</span>}{item.blocked_devices.length > 0 && <span>{t("设备 ")}{item.blocked_devices.length}</span>}{!ruleCount(item) && <span className="cloak-no-rule">{t("未设置条件")}</span>}</div></td><td><span className={`cloak-status-badge status-${statusOf(item)}`}><i />{statusLabel(statusOf(item))}</span></td><td><div className="row-actions wrap"><VisitStatisticsButton kind="cloaks" id={item.id} name={item.name}/><button className="text-btn" onClick={() => start(item)}><Pencil size={14} aria-hidden="true" />{t("编辑")}</button><StatusDialogButton item={item} disabled={busy} onChange={next => changeStatus(item, next)} /><button className="text-btn danger-text" disabled={busy} onClick={() => { setMessage(''); setDeleting(item); }}><Trash2 size={14} aria-hidden="true" />{t("删除")}</button></div></td></tr>)}</tbody></table></div>}</section>
    <p className="cloak-bottom-note"><ShieldOff size={14} />{t("按规则：检查已设置的条件；关闭：跳过全部斗篷规则；屏蔽：全部访问进入安全页。")}</p>
    {sharing && <DomainShareDrawer path={`/c/${sharing.slug}`} close={() => setSharing(undefined)} />}
    {deleting && <Modal title={t("删除斗篷？")} close={() => { if (!busy) setDeleting(undefined); }}><p>{t("删除「")}{deleting.name}{t("」后，斗篷入口将停止访问。")}</p><FormError>{message}</FormError><div className="modal-footer"><button className="btn secondary" disabled={busy} onClick={() => setDeleting(undefined)}>{t("取消")}</button><button className="btn danger" disabled={busy} onClick={remove}>{t("删除斗篷")}</button></div></Modal>}
  </div>;
}
