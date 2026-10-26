import { useRef, useState, type FormEvent } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNotify } from 'react-admin';
import { Trash2 } from 'lucide-react';
import { send } from './api';
import { t, useLocale } from './i18n';
import type { User } from './types';
import { errorText, Field, FormError, Modal, money } from './ui';

export function AdminDeleteUser({ user, close, deleted }: { user: User; close: () => void; deleted: () => void }) {
  useLocale();
  const [email, setEmail] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const submitting = useRef(false);
  const cache = useQueryClient();
  const notify = useNotify();
  const confirmed = email.trim().toLowerCase() === user.email.toLowerCase();
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (submitting.current || !confirmed) return;
    submitting.current = true;
    setBusy(true); setError('');
    try {
      await send(`/admin/users/${user.id}`, { confirm_email: email.trim() }, 'DELETE');
      void cache.invalidateQueries();
      notify(t('用户已删除'), { type: 'success' });
      deleted();
    } catch (error) { setError(errorText(error)); }
    finally { submitting.current = false; setBusy(false); }
  };
  return <Modal title={t('确认删除用户？')} close={() => { if (!busy) close(); }}><form onSubmit={submit}>
    <p className="modal-intro"><strong>{user.name}</strong> · {user.email}<br/>{t('当前余额 $')}{money(user.balance_micros, 3)}</p>
    <p className="notice compact">{t('删除后，该用户将无法登录，关联的 APK、PWA、分流和斗篷入口会停用。订单、资金流水和审计记录保留。')}</p>
    <p className="small muted">{t('账户余额、冻结用量、未完成的充值订单和提现须先处理完成。')}</p>
    <Field label={t('输入用户邮箱确认删除')}><input required type="email" maxLength={254} autoComplete="off" spellCheck={false} disabled={busy} placeholder={user.email} value={email} onChange={event => { setEmail(event.target.value); setError(''); }}/></Field>
    <FormError>{error}</FormError>
    <div className="modal-footer"><button type="button" className="btn secondary" disabled={busy} onClick={close}>{t('取消')}</button><button type="submit" className="btn danger" disabled={busy || !confirmed}><Trash2 size={15}/>{busy ? t('删除中…') : t('删除用户')}</button></div>
  </form></Modal>;
}
