import { useState, type FormEvent } from 'react';
import { ShieldCheck, Wallet } from 'lucide-react';
import { send } from './api';
import type { SettingsResponse } from './types';
import { Badge, errorText, Field, FormError } from './ui';

export function UsdtWalletSettings({ data, saved }: { data: SettingsResponse; saved: (data: SettingsResponse) => void }) {
  const current = String(data.runtime.usdt_wallet || '');
  const [wallet, setWallet] = useState(current);
  const [password, setPassword] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [success, setSuccess] = useState(false);
  const secured = data.runtime.usdt_address_password_configured === true;
  const submit = async (event: FormEvent) => {
    event.preventDefault(); setBusy(true); setError(''); setSuccess(false);
    try {
      const result = await send<SettingsResponse>('/admin/usdt-wallet', { wallet, secondary_password: password }, 'PUT');
      setWallet(String(result.runtime.usdt_wallet || '')); saved(result); setSuccess(true);
    } catch (e) { setError(errorText(e)); }
    finally { setPassword(''); setBusy(false); }
  };
  return <section className="panel padded settings-panel">
    <h2><Wallet size={19} />USDT 收款配置</h2>
    <p className="small muted">仅 TRON / TRC20 主网。填写你现有 UZF 实际监控账户的收款地址，不需要钱包私钥。新地址仅用于新订单，已有订单保持原地址。</p>
    <p><Badge tone={data.runtime.usdt_provider === 'uzf' ? 'violet' : 'gray'}>{data.runtime.usdt_provider === 'uzf' ? 'UZF / OKX 到账监控' : 'TRON 链上核对'}</Badge></p>
    <form onSubmit={submit}>
      <Field label="USDT 收款地址（TRC20）" hint="留空并保存会关闭新 USDT 充值订单。填写后请核对完整地址，勿仅核对首尾字符。">
        <input aria-label="USDT 收款地址（TRC20）" value={wallet} onChange={e => { setWallet(e.target.value); setSuccess(false); }} maxLength={34} autoComplete="off" spellCheck={false} placeholder="T 开头的 34 位地址" />
      </Field>
      <Field label="收款地址二级密码" hint="每次保存都必须验证，密码不会回显或写入操作日志。">
        <input aria-label="收款地址二级密码" type="password" required maxLength={72} autoComplete="off" value={password} onChange={e => setPassword(e.target.value)} />
      </Field>
      {!secured && <p className="notice compact"><ShieldCheck size={16} />二级密码尚未在服务器配置，暂不能更改收款地址。</p>}
      <FormError>{error}</FormError>
      <div className="settings-save-row"><button className="btn primary" disabled={busy || !secured}>{busy ? '验证并保存中…' : '验证二级密码并保存地址'}</button><span role="status" className="settings-feedback success">{success ? '收款地址已保存，仅对新订单生效' : ''}</span></div>
    </form>
    <p className="small muted">D1 只连接现有 UZF 查询接口，不部署或修改 UZF。查询地址和密钥通过服务器环境配置；未配置时暂停 USDT 新订单。到账按原接口返回的账单编号、完整金额和付款时间核对，同一账单不能重复入账。</p>
  </section>;
}
