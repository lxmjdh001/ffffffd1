import { ArrowUpRight, MessageCircle } from 'lucide-react';
import { useState } from 'react';
import { t, useLocale } from './i18n';
import { Modal, useConfig } from './ui';
import './home-contact.css';

export function supportLinkURL(raw?: string): string | undefined {
  const value = raw?.trim();
  if (!value || value.length > 2048 || /[\s\u0000-\u001f\u007f\\]/u.test(value)) return;
  try {
    const url = new URL(value);
    if (['http:', 'https:'].includes(url.protocol) && url.hostname && !url.username && !url.password) return value;
  } catch { /* Unconfigured or invalid links do not create an unusable button. */ }
}

export function HomeContact({ shine = false, onClick }: { shine?: boolean; onClick?: () => void }) {
  useLocale();
  const { config } = useConfig();
  const [unavailable, setUnavailable] = useState(false);
  const url = supportLinkURL(config.support_url);
  const className = `home-contact${shine ? ' home-contact-shine' : ''}`;
  const label = <><MessageCircle size={16} aria-hidden="true"/><span>{t('联系商务')}</span><ArrowUpRight size={15} aria-hidden="true"/></>;
  if (url) return <a className={className} href={url} target="_blank" rel="noopener noreferrer" onClick={onClick}>{label}</a>;
  return <><button type="button" className={className} aria-haspopup="dialog" onClick={() => setUnavailable(true)}>{label}</button>
    {unavailable && <Modal title={t('联系商务')} close={() => setUnavailable(false)}><p className="modal-intro">{t('联系入口暂未配置，请稍后再试。')}</p><div className="modal-footer"><button type="button" className="btn secondary" onClick={() => setUnavailable(false)}>{t('关闭')}</button></div></Modal>}
  </>;
}
