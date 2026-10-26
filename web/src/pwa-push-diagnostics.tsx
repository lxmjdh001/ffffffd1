import { t } from './i18n';
import { Badge, number } from './ui';
import type { PWAPushDelivery } from './pwa-types';

export function pushDeliveryStage(delivery: PWAPushDelivery) {
  if (delivery.clicked_at) return 'clicked';
  if (delivery.displayed_at) return 'displayed';
  if (delivery.sent_at || delivery.status === 'sent') return 'accepted';
  return delivery.status;
}

export function PushDeliveryStage({ delivery }: { delivery: PWAPushDelivery }) {
  const stage = pushDeliveryStage(delivery);
  const label = ({ pending: t('等待发送'), processing: t('发送中'), accepted: t('推送服务已接受'), displayed: t('设备已显示'), clicked: t('用户已点击'), failed: t('发送失败'), cancelled: t('已取消') } as Record<string, string>)[stage] || t('未知状态');
  return <Badge tone={stage === 'clicked' || stage === 'displayed' ? 'green' : stage === 'accepted' ? 'blue' : stage === 'failed' ? 'red' : 'gray'}>{label}</Badge>;
}

function reason(code?: string) {
  switch (code) {
    case 'network_timeout': return t('连接推送服务超时');
    case 'dns_failed': return t('推送服务域名解析失败');
    case 'tls_failed': return t('推送服务安全连接失败');
    case 'network_failed': return t('无法连接推送服务');
    case 'auth_failed': case 'vapid_key_invalid': return t('推送身份验证失败，请检查密钥与订阅是否匹配。');
    case 'subscription_expired': case 'subscription_inactive': return t('订阅已失效或关闭，需要用户重新开启通知。');
    case 'subscription_invalid': case 'subscription_key_invalid': return t('订阅信息不可用，需要用户重新开启通知。');
    case 'payload_too_large': return t('通知内容过大，请缩短正文或链接。');
    case 'invalid_request': case 'payload_invalid': return t('通知请求无效，请检查内容和推送配置。');
    case 'rate_limited': return t('推送服务限流');
    case 'provider_unavailable': return t('推送服务暂时不可用');
    case 'delivery_expired': return t('通知重试期限已结束');
    case 'app_disabled': case 'app_removed': return t('应用或账户已停用通知。');
    default: return t('发送未完成，请检查应用和订阅状态。');
  }
}

export function PushDeliveryDiagnostic({ delivery, date }: { delivery: PWAPushDelivery; date: (value?: string | null) => string }) {
  const provider = ({ apple: 'Apple', google: 'Google FCM', mozilla: 'Mozilla', microsoft: 'Microsoft' } as Record<string, string>)[delivery.provider || ''];
  const failed = delivery.status === 'failed' || (delivery.status === 'pending' && delivery.attempts > 0);
  const http = Number.isInteger(delivery.provider_status) && delivery.provider_status! >= 100 && delivery.provider_status! <= 599 ? `HTTP ${delivery.provider_status}` : '';
  return <div className="push-delivery-diagnostic">
    {(provider || http) && <span className="push-provider">{[provider, http].filter(Boolean).join(' · ')}</span>}
    {failed && <span className="push-diagnostic-reason">{reason(delivery.error_code)}</span>}
    {delivery.attempts > 0 && <small>{t('尝试 {0} 次', { 0: number(delivery.attempts) })}{delivery.last_attempt_at && <> · {date(delivery.last_attempt_at)}</>}</small>}
    {delivery.status === 'pending' && delivery.attempts > 0 && delivery.next_attempt_at && <small>{t('下次重试：{0}', { 0: date(delivery.next_attempt_at) })}</small>}
    {(delivery.sent_at || delivery.status === 'sent' || delivery.clicked_at) && !delivery.displayed_at && <small>{t('尚未收到设备展示回执')}</small>}
  </div>;
}
