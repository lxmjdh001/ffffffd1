// i18n: static UI literals
import { t, useLocale } from './i18n';
import type { Dispatch, SetStateAction, ReactNode } from 'react';
import { Globe2, Plus, Trash2 } from 'lucide-react';
import type { SettingsResponse } from './types';
import { Field } from './ui';

export function DownloadDomainRules({ form, setForm, footer }: { form: SettingsResponse; setForm: Dispatch<SetStateAction<SettingsResponse | undefined>>; footer: ReactNode }) {
  useLocale();
  const rules = form.settings.download_domain_rules || [];
  const update = (next: typeof rules) => setForm({ ...form, settings: { ...form.settings, download_domain_rules: next } });
  return <section className="panel padded settings-panel">
    <h2><Globe2 size={19} />{t("下载域名配置")}</h2>
    <p className="muted small">{t("每个域名单独设置访问权限，可同时添加多个域名。“仅保留旧链接”的域名不会再分配或展示，已有链接继续可用。")}</p>
    {rules.map((rule, i) => <div key={i}>
      <div className="form-grid domain-rules-grid">
        <Field label={t("域名 {0}", { 0: i + 1 })}><input placeholder="https://www.a.com" value={rule.url} onChange={e => update(rules.map((x, j) => j === i ? { ...x, url: e.target.value } : x))} /></Field>
        <Field label={t("备注")}><input maxLength={120} placeholder={t("备注")} value={rule.remark || ''} onChange={e => update(rules.map((x, j) => j === i ? { ...x, remark: e.target.value } : x))} /></Field>
        <Field label={t("权限")}><div style={{ display: 'flex', gap: 8 }}>
          <select value={rule.audience} onChange={e => update(rules.map((x, j) => j === i ? { ...x, audience: e.target.value as 'all' | 'paid' } : x))}><option value="all">{t("所有用户")}</option>{!form.settings.metered_billing && <option value="paid">{t("付费用户")}</option>}</select>
          <button type="button" className="btn secondary" aria-label={t("删除域名 {0}", { 0: i + 1 })} onClick={() => update(rules.filter((_, j) => j !== i))}><Trash2 size={15} /></button>
        </div></Field>
      </div>
      <label className="switch-field"><input type="checkbox" checked={!!rule.legacy_only} onChange={e => update(rules.map((x, j) => j === i ? { ...x, legacy_only: e.target.checked } : x))} /><span>{t("仅保留旧链接")}<small>{rule.url || t("域名 {0}", { 0: i + 1 })} {t(" · 不再分配，不在复制列表展示")}</small></span></label>
    </div>)}
    <div style={{ display: 'flex', gap: 10 }}><button type="button" className="btn secondary" onClick={() => update([...rules, { url: '', audience: 'all' }])}><Plus size={15} />{t("添加域名")}</button></div>
    {footer}
  </section>;
}
