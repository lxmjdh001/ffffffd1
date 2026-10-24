import { useState } from 'react';
import { useQueryClient } from '@tanstack/react-query';
import { useNotify } from 'react-admin';
import { api } from './api';
import type { App } from './types';
import { errorText, FormError, Modal } from './ui';

export function AppDeleteDialog({ app, close, deleted }: { app: App; close: () => void; deleted: () => void }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const cache = useQueryClient();
  const notify = useNotify();
  const remove = async () => {
    if (busy) return;
    setBusy(true);
    setError('');
    try {
      const result = await api<{ ok: boolean; file_cleanup_pending?: boolean }>(`/apps/${app.id}`, { method: 'DELETE' });
      cache.invalidateQueries();
      notify(result.file_cleanup_pending ? '应用已永久删除，安装包或图片清理暂未完成，后台将重试清理' : '应用、安装包及上传图片已永久删除', { type: result.file_cleanup_pending ? 'warning' : 'success' });
      deleted();
    } catch (e) {
      setError(errorText(e));
    } finally {
      setBusy(false);
    }
  };
  return <Modal title="确定永久删除这个应用？" close={() => { if (!busy) close(); }}>
    <p>将永久删除「{app.name}」的应用数据、安装包及其上传图片，释放应用名额与存储空间，所有分享链接立即失效。此操作无法恢复。</p>
    <p className="small muted">历史下载记录与账单保留，不影响已完成的结算。</p>
    <FormError>{error}</FormError>
    <div className="modal-footer">
      <button className="btn secondary" disabled={busy} onClick={close}>取消</button>
      <button className="btn danger" disabled={busy} onClick={remove}>{busy ? '正在删除…' : '确认永久删除'}</button>
    </div>
  </Modal>;
}
