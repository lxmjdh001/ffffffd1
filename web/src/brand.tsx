import { useLocale } from './i18n';
import { useConfig } from './ui';

export function Brand({ dark = false }: { dark?: boolean }) {
  useLocale(); const { config } = useConfig(); return <span className={`brand ${dark ? 'on-dark' : ''}`}><img src="/favicon.svg" alt="" /><span>{config.site_name}</span></span>; }
