import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ArrowUpRight, Gift, X } from 'lucide-react';
import { APIError, getMe } from './api';
import { t, useLocale } from './i18n';
import { useConfig } from './ui';
import { usagePrice } from './usage-billing';
import './registration-gift.css';

const dismissalKey = 'd1.registration-gift.dismissed';
export function RegistrationGift({ registerPath }: { registerPath: string }) {
 useLocale();
 const { config } = useConfig();
 const amount = config.registration_gift_micros ?? 100e6;
 const [visible, setVisible] = useState(false);
 useEffect(() => {
  if (!config.registration_open || amount <= 0) return;
  try { if (sessionStorage.getItem(dismissalKey)) return; } catch { /* Storage may be disabled. */ }
  const controller = new AbortController();
  let timer: ReturnType<typeof setTimeout> | undefined;
  void getMe({ signal: controller.signal }).catch(error => {
   if (!controller.signal.aborted && error instanceof APIError && error.status === 401) timer = setTimeout(() => setVisible(true), 1200);
  });
  return () => { controller.abort(); clearTimeout(timer); };
 }, [config.registration_open, amount]);
 const dismiss = () => { setVisible(false); try { sessionStorage.setItem(dismissalKey, '1'); } catch { /* Keep dismissal in component state. */ } };
 if (!visible || !config.registration_open || amount <= 0) return null;
 return <aside className="registration-gift" aria-label={t('注册赠送用量额度')} onKeyDown={event => { if (event.key === 'Escape') dismiss(); }}>
  <button type="button" className="registration-gift-close" aria-label={t('关闭')} onClick={dismiss}><X size={17}/></button>
  <div className="registration-gift-kicker"><span><Gift size={19}/></span>WELCOME GIFT</div>
  <h2>{t('注册赠送用量额度')}</h2>
  <div className="registration-gift-amount"><span>$</span>{usagePrice(amount)}<span className="registration-gift-spark" aria-hidden="true">✳</span></div>
  <p>{t('赠送用量额度')}</p>
  <Link to={registerPath} onClick={dismiss}>{t('免费创建账户')}<ArrowUpRight size={18}/></Link>
 </aside>;
}
