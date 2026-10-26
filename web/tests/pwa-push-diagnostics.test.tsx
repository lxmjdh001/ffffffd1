import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { PushDeliveryDiagnostic, PushDeliveryStage, pushDeliveryStage } from '../src/pwa-push-diagnostics';
import type { PWAPushDelivery } from '../src/pwa-types';

const time = '2026-10-26T01:00:00Z';
const row: PWAPushDelivery = { id: 'one', task_id: 'task', subscription_id: 'subscription', country: 'US', status: 'sent', attempts: 1, last_error: 'private endpoint must never be displayed', created_at: time, sent_at: time, displayed_at: null, clicked_at: null };
afterEach(cleanup);

describe('Web Push delivery diagnostics', () => {
  it.each([
    { ...row, expected: 'accepted', label: '推送服务已接受' },
    { ...row, displayed_at: time, expected: 'displayed', label: '设备已显示' },
    { ...row, clicked_at: time, expected: 'clicked', label: '用户已点击' },
  ])('reports $expected only from its actual delivery evidence', delivery => {
    expect(pushDeliveryStage(delivery)).toBe(delivery.expected);
    render(<PushDeliveryStage delivery={delivery} />);
    expect(screen.getByText(delivery.label)).toBeTruthy();
  });

  it('shows accepted HTTP status and missing display receipt without claiming a failure', () => {
    render(<PushDeliveryDiagnostic delivery={{ ...row, provider: 'apple', provider_status: 201 }} date={value => value || '—'} />);
    expect(screen.getByText('Apple · HTTP 201')).toBeTruthy();
    expect(screen.getByText('尚未收到设备展示回执')).toBeTruthy();
    expect(screen.queryByText(/private endpoint|发送未完成/)).toBeNull();
  });

  it('shows a safe auth diagnosis and HTTP status instead of raw provider text', () => {
    render(<PushDeliveryDiagnostic delivery={{ ...row, status: 'failed', sent_at: null, provider: 'google', provider_status: 403, error_code: 'auth_failed' }} date={value => value || '—'} />);
    expect(screen.getByText('Google FCM · HTTP 403')).toBeTruthy();
    expect(screen.getByText('推送身份验证失败，请检查密钥与订阅是否匹配。')).toBeTruthy();
    expect(screen.queryByText(/private endpoint|尚未收到设备展示回执/)).toBeNull();
  });

  it('shows scheduled retries for rate limiting and keeps legacy errors private', () => {
    render(<PushDeliveryDiagnostic delivery={{ ...row, status: 'pending', sent_at: null, error_code: 'rate_limited', attempts: 2, next_attempt_at: time, last_attempt_at: time }} date={value => value || '—'} />);
    expect(screen.getByText('推送服务限流')).toBeTruthy();
    expect(screen.getByText(`下次重试：${time}`)).toBeTruthy();
    expect(screen.getByText(/尝试 2 次/)).toBeTruthy();
    expect(screen.queryByText(/private endpoint/)).toBeNull();
  });

  it('does not fabricate a display receipt from a click and ignores unrecognized provider strings', () => {
    render(<PushDeliveryDiagnostic delivery={{ ...row, provider: 'secret subscription endpoint', clicked_at: time }} date={value => value || '—'} />);
    expect(screen.getByText('尚未收到设备展示回执')).toBeTruthy();
    expect(screen.queryByText(/secret subscription/)).toBeNull();
  });
});
