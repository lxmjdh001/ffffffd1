import { useCallback, useEffect, useState } from 'react';
import { Admin, CustomRoutes, Resource } from 'react-admin';
import { Route } from 'react-router-dom';
import { api, authProvider, dataProvider } from './api';
import type { Config } from './types';
import { ConfigContext, ErrorState, Spinner } from './ui';
import { AuthPage } from './auth';
import { LandingPage } from './landing';
import { AppLayout } from './layout';
import { DashboardPage } from './dashboard';
import { AppsPage, AppDetailPage } from './apps';
import { PageDesigner } from './page-designer';
import { DomainsPage } from './domains';
import { TrafficPage } from './traffic';
import { CloaksPage } from './cloaks';
import { BillingPage } from './billing';
import { ReferralsPage } from './referrals';
import { ActivityPage } from './activity';
import { AccountPage } from './account';
import { AdminListPage, AdminSettingsPage } from './admin';
import { HelpPage } from './help';
import { AdsPage, IntegrationsPage } from './ads';

export function Root() {
  const [config, setConfig] = useState<Config>(); const [error, setError] = useState<unknown>();
  const reload = useCallback(() => { api<Config>('/config').then(c => { setConfig(c); document.title = `${c.site_name} · 应用分发平台`; setError(undefined); }).catch(setError); }, []);
  useEffect(reload, [reload]);
  if (error) return <ErrorState error={error} retry={reload} />; if (!config) return <Spinner />;
  return <ConfigContext.Provider value={{ config, reload }}><Admin title={config.site_name} dataProvider={dataProvider} authProvider={authProvider} dashboard={DashboardPage} layout={AppLayout} loginPage={AuthPage} requireAuth disableTelemetry theme={{ palette: { mode: 'light', primary: { main: '#6373e8' } }, typography: { fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif' } }}><Resource name="apps" list={AppsPage} /><CustomRoutes><Route path="/apps/:id" element={<AppDetailPage />} /><Route path="/apps/:id/design" element={<PageDesigner />} /><Route path="/apps/:id/ads" element={<AdsPage />} /><Route path="/integrations" element={<IntegrationsPage />} /><Route path="/domains" element={<DomainsPage />} /><Route path="/traffic" element={<TrafficPage />} /><Route path="/cloaks" element={<CloaksPage />} /><Route path="/billing" element={<BillingPage />} /><Route path="/referrals" element={<ReferralsPage />} /><Route path="/activity" element={<ActivityPage />} /><Route path="/account" element={<AccountPage />} /><Route path="/help" element={<HelpPage />} /><Route path="/admin/users" element={<AdminListPage key="users" resource="users" />} /><Route path="/admin/apps" element={<AdminListPage key="apps" resource="apps" />} /><Route path="/admin/orders" element={<AdminListPage key="orders" resource="orders" />} /><Route path="/admin/audit" element={<AdminListPage key="audit" resource="audit" />} /><Route path="/admin/settings" element={<AdminSettingsPage />} /></CustomRoutes><CustomRoutes noLayout><Route path="/register" element={<AuthPage register />} /><Route path="/welcome" element={<LandingPage />} /></CustomRoutes></Admin></ConfigContext.Provider>;
}
