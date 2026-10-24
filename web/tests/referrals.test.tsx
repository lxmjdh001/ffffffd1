import { cleanup, render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ReferralsPage } from '../src/referrals';
import type { Referrals } from '../src/types';

const inviteCode = '0123456789abcdef';
const inviteURL = `https://console.example/#/register?invite=${inviteCode}`;
const response: Referrals = {
  invite_code: inviteCode, invite_url: inviteURL, rates: [10, 2, 2],
  total_reward_micros: 14000000, team_count: 3, direct_count: 1, level_counts: [1, 1, 1],
  rewards: { data: [{ id: 'reward-1', level: 1, amount_micros: 10000000, recharge_micros: 100000000, created_at: '2026-10-24T03:00:00Z', source_name: '一级充值好友' }], total: 11 },
  members: { data: [{ id: 'member-1', name: '一级团队好友', level: 1, created_at: '2026-10-24T01:00:00Z' }], total: 11 },
};
let client: QueryClient;
let requests: URLSearchParams[];
let empty = false;
let fail = false;

beforeEach(() => {
  client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  requests = []; empty = false; fail = false;
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
    const url = new URL(String(input), 'https://console.example');
    if (url.pathname !== '/api/referrals') throw new Error(`Unexpected request: ${input}`);
    requests.push(url.searchParams);
    if (fail) return new Response(JSON.stringify({ error: '邀请记录加载失败' }), { status: 503 });
    const data = structuredClone(response);
    if (empty) { data.rewards = { data: [], total: 0 }; data.members = { data: [], total: 0 }; }
    if (url.searchParams.get('rewards_page') === '2') data.rewards.data = [{ ...response.rewards.data[0], id: 'reward-11', source_name: '第二页充值好友', level: 3, amount_micros: 2000100, recharge_micros: 100005000 }];
    if (url.searchParams.get('members_page') === '2') data.members.data = [{ ...response.members.data[0], id: 'member-11', name: '第二页团队好友', level: 2 }];
    return new Response(JSON.stringify(data));
  }));
});
afterEach(() => { cleanup(); client.clear(); vi.unstubAllGlobals(); vi.restoreAllMocks(); });

function open() {
  render(<QueryClientProvider client={client}><ReferralsPage /></QueryClientProvider>);
  return userEvent.setup();
}

describe('referrals', () => {
  it('shows the permanent three-level rules, balance credit and share values', async () => {
    open();
    await screen.findByRole('heading', { name: '邀请好友' });
    expect((screen.getByRole('textbox', { name: '我的邀请码' }) as HTMLInputElement).value).toBe(inviteCode);
    expect((screen.getByRole('textbox', { name: '邀请链接' }) as HTMLInputElement).value).toBe(inviteURL);
    expect(screen.getByText('$ 14.00')).toBeTruthy();
    expect(screen.getByText(/邀请关系长期有效。好友每次充值到账后/)).toBeTruthy();
    expect(screen.getByText(/一级好友充值 \$100，你获得 \$10.00/)).toBeTruthy();
    expect(screen.getByText('+ $ 10.00')).toBeTruthy();
    expect(screen.getByText('10')).toBeTruthy();
    expect(screen.getAllByText('2')).toHaveLength(2);
    expect(requests[0].get('per_page')).toBe('10');
  });

  it('copies the invite code and URL and leaves manual selection available after clipboard failure', async () => {
    const user = open();
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue(undefined);
    await user.click(await screen.findByRole('button', { name: '复制邀请码' }));
    expect(writeText).toHaveBeenLastCalledWith(inviteCode);
    expect(screen.getByRole('status').textContent).toContain('邀请码已复制');
    await user.click(screen.getByRole('button', { name: '复制链接' }));
    expect(writeText).toHaveBeenLastCalledWith(inviteURL);
    expect(screen.getByRole('status').textContent).toContain('邀请链接已复制');
    writeText.mockRejectedValueOnce(new Error('Clipboard denied'));
    await user.click(screen.getByRole('button', { name: '复制链接' }));
    expect(screen.getByRole('alert').textContent).toContain('复制失败');
    expect(screen.queryByRole('status')).toBeNull();
    const field = screen.getByRole('textbox', { name: '邀请链接' }) as HTMLInputElement;
    await user.click(field);
    expect(field.selectionStart).toBe(0);
    expect(field.selectionEnd).toBe(inviteURL.length);
  });

  it('paginates reward and team lists independently with precise fractional credits', async () => {
    const user = open();
    const rewards = within(await screen.findByRole('region', { name: '返利明细' }));
    const members = within(screen.getByRole('region', { name: '我的团队' }));
    expect(rewards.getByText('1 / 2')).toBeTruthy();
    expect(members.getByText('1 / 2')).toBeTruthy();
    await waitFor(() => expect((rewards.getByRole('button', { name: '下一页' }) as HTMLButtonElement).matches(':disabled')).toBe(false));
    await user.click(rewards.getByRole('button', { name: '下一页' }));
    await rewards.findByText('第二页充值好友');
    expect(rewards.getByText('+ $ 2.0001')).toBeTruthy();
    expect(rewards.getByText('$ 100.005')).toBeTruthy();
    expect(rewards.getByText('三级好友')).toBeTruthy();
    expect(members.getByText('一级团队好友')).toBeTruthy();
    await user.click(members.getByRole('button', { name: '下一页' }));
    await members.findByText('第二页团队好友');
    expect(rewards.getByText('第二页充值好友')).toBeTruthy();
    expect(rewards.getByText('2 / 2')).toBeTruthy();
    expect(members.getByText('2 / 2')).toBeTruthy();
    expect(requests.map(params => [params.get('rewards_page'), params.get('members_page')])).toEqual([['1', '1'], ['2', '1'], ['2', '2']]);
  });

  it('shows useful empty states for a new inviter', async () => {
    empty = true; open();
    expect(await screen.findByText('还没有返利记录')).toBeTruthy();
    expect(screen.getByText('还没有邀请好友')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: '下一页' }).every(button => (button as HTMLButtonElement).disabled)).toBe(true);
  });

  it('recovers an API error after retry', async () => {
    fail = true;
    const user = open();
    expect(await screen.findByText('邀请记录加载失败')).toBeTruthy();
    fail = false;
    await user.click(screen.getByRole('button', { name: '重新加载' }));
    expect(await screen.findByRole('heading', { name: '分享你的专属邀请' })).toBeTruthy();
  });
});
