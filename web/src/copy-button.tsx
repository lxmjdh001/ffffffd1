import { Copy } from 'lucide-react';
import { useNotify } from 'react-admin';
import { t, useLocale } from './i18n';

export function CopyButton({ value, label }: { value: string; label?: string }) {
  useLocale(); const notify = useNotify(); return <button className="copy-btn" title={label || t("复制")} onClick={async () => { try { await navigator.clipboard.writeText(value); notify(t("已复制"), { type: 'success' }); } catch { notify(t("复制失败，请手动选择复制"), { type: 'warning' }); } }}><Copy size={14} />{label}</button>; }
