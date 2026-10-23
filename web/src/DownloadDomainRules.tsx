import type { Dispatch, SetStateAction, ReactNode } from 'react';
import { Globe2, Plus, Trash2 } from 'lucide-react';
import type { SettingsResponse } from './types';
import { Field } from './ui';

export function DownloadDomainRules({ form, setForm, footer }: { form: SettingsResponse; setForm: Dispatch<SetStateAction<SettingsResponse | undefined>>; footer: ReactNode }) {
  const rules = form.settings.download_domain_rules || [];
  const update = (next: typeof rules) => setForm({ ...form, settings: { ...form.settings, download_domain_rules: next } });
  return <section className="panel padded settings-panel"><h2><Globe2 size={19} />下载域名配置</h2><p className="muted small">每个域名单独设置访问权限，可同时添加多个域名。</p>{rules.map((rule, i) => <div className="form-grid domain-rules-grid" key={i}><Field label={`域名 ${i + 1}`}><input placeholder="https://www.a.com" value={rule.url} onChange={e => update(rules.map((x, j) => j === i ? { ...x, url: e.target.value } : x))} /></Field><Field label="备注"><input maxLength={120} placeholder="例如：主域名 / 付费专线" value={rule.remark || ''} onChange={e => update(rules.map((x, j) => j === i ? { ...x, remark: e.target.value } : x))} /></Field><Field label="权限"><div style={{ display: 'flex', gap: 8 }}><select value={rule.audience} onChange={e => update(rules.map((x, j) => j === i ? { ...x, audience: e.target.value as 'all' | 'paid' } : x))}><option value="all">所有用户</option><option value="paid">付费用户</option></select><button type="button" className="btn secondary" aria-label={`删除域名 ${i + 1}`} onClick={() => update(rules.filter((_, j) => j !== i))}><Trash2 size={15} /></button></div></Field></div>)}<div style={{ display: 'flex', gap: 10 }}><button type="button" className="btn secondary" onClick={() => update([...rules, { url: '', audience: 'all' }])}><Plus size={15} />添加域名</button></div>{footer}</section>;
}
