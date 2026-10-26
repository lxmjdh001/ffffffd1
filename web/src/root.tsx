// i18n: static UI literals
import { useLocale } from './i18n';
import { useCallback, useEffect, useState, type ComponentProps, type ReactNode } from 'react';
import { Admin, CustomRoutes, Resource } from 'react-admin';
import { Route } from 'react-router-dom';
import { api, authProvider, dataProvider } from './api';
import type { Config } from './types';
import { ConfigContext, ErrorState, Spinner, useConfig } from './ui';
import { AuthPage } from './auth';
import { LandingPage } from './landing';
import { AppLayout } from './layout';
import { DashboardPage } from './dashboard';
import { AppsPage, AppDetailPage, AppCreatePage } from './apps';
import { PageDesigner } from './page-designer';
import { DomainsPage } from './domains';
import { TrafficPage } from './traffic';
import { CloaksPage } from './cloaks';
import { PWAsPage, PWAEditorPage } from './pwas';
import { PWAPushPage } from './pwa-push';
import { DIYLandingsPage } from './diy-landings';
import { LandingTemplateLibraryPage, AdminLandingTemplateLibraryPage } from './landing-template-library';
import { BillingPage } from './billing';
import { ReferralsPage } from './referrals';
import { AdminReferralWithdrawalsPage } from './referral-withdrawals';
import { CommissionSettingsPage } from './commission-settings';
import { ActivityPage } from './activity';
import { AccountPage } from './account';
import { AdminListPage, AdminSettingsPage } from './admin';
import { HelpPage, DIYLandingGuidePage } from './help';
import { AdsPage, IntegrationsPage } from './ads';
import { RouteMetadata, usePageMetadata } from './home-seo';
import { initializeLocale } from './i18n';

function MetadataPage({ children }: { children: ReactNode }) {
  useLocale();
  const { config } = useConfig();
  return <><RouteMetadata siteName={config.site_name} publicURL={config.public_url} />{children}</>;
}
function MetadataAuthPage() {
  useLocale(); return <MetadataPage><AuthPage /></MetadataPage>; }
function MetadataLayout(props: ComponentProps<typeof AppLayout>) {
  useLocale(); return <MetadataPage><AppLayout {...props} /></MetadataPage>; }

declare global {
  interface Window { __d1HomeConfig?: Promise<Config | undefined> }
}

export function Root({ initialHomeHTML = '' }: { initialHomeHTML?: string }) {
  useLocale();
  const [config, setConfig] = useState<Config>(); const [error, setError] = useState<unknown>();
  const reload = useCallback(() => {
    const prefetched = window.__d1HomeConfig;
    const request = prefetched ? prefetched.then(c => c ?? api<Config>('/config')) : api<Config>('/config');
    request.then(c => { delete window.__d1HomeConfig; initializeLocale(c.country_code); setConfig(c); setError(undefined); }).catch(failure => { delete window.__d1HomeConfig; setError(failure); });
  }, []);
  useEffect(reload, [reload]);
  usePageMetadata(config?.site_name, config?.public_url);
  if (error) return <ErrorState error={error} retry={reload} />;
  if (!config) {
    const home = !window.location.hash || window.location.hash === '#' || /^#\/welcome(?:\?|$)/.test(window.location.hash);
    return home && initialHomeHTML ? <div dangerouslySetInnerHTML={{ __html: initialHomeHTML }} /> : <Spinner />;
  }
  return <ConfigContext.Provider value={{ config, reload }}><Admin title={config.site_name} dataProvider={dataProvider} authProvider={authProvider} dashboard={DashboardPage} layout={MetadataLayout} loginPage={MetadataAuthPage} requireAuth disableTelemetry theme={{ palette: { mode: 'light', primary: { main: '#6373e8' } }, typography: { fontFamily: 'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", sans-serif' } }}><Resource name="apps" list={AppsPage} /><CustomRoutes><Route path="/apps/new" element={<AppCreatePage />} /><Route path="/apps/:id" element={<AppDetailPage />} /><Route path="/apps/:id/design" element={<PageDesigner />} /><Route path="/apps/:id/ads" element={<AdsPage />} /><Route path="/integrations" element={<IntegrationsPage />} /><Route path="/domains" element={<DomainsPage />} /><Route path="/traffic" element={<TrafficPage />} /><Route path="/cloaks" element={<CloaksPage />} /><Route path="/pwas" element={<PWAsPage />} /><Route path="/pwas/new" element={<PWAEditorPage />} /><Route path="/pwas/:id/edit" element={<PWAEditorPage />} /><Route path="/diy-landings" element={<DIYLandingsPage />} /><Route path="/landing-templates" element={<LandingTemplateLibraryPage />} /><Route path="/admin/landing-templates" element={<AdminLandingTemplateLibraryPage />} /><Route path="/diy-landings/:id/edit" element={<DIYLandingsPage />} /><Route path="/pwa-push" element={<PWAPushPage />} /><Route path="/billing" element={<BillingPage />} /><Route path="/referrals" element={<ReferralsPage />} /><Route path="/activity" element={<ActivityPage />} /><Route path="/account" element={<AccountPage />} /><Route path="/help" element={<HelpPage />} /><Route path="/help/diy-landings" element={<DIYLandingGuidePage />} /><Route path="/admin/users" element={<AdminListPage key="users" resource="users" />} /><Route path="/admin/apps" element={<AdminListPage key="apps" resource="apps" />} /><Route path="/admin/orders" element={<AdminListPage key="orders" resource="orders" />} /><Route path="/admin/commissions" element={<CommissionSettingsPage />} /><Route path="/admin/referral-withdrawals" element={<AdminReferralWithdrawalsPage />} /><Route path="/admin/audit" element={<AdminListPage key="audit" resource="audit" />} /><Route path="/admin/settings" element={<AdminSettingsPage />} /></CustomRoutes><CustomRoutes noLayout><Route path="/register" element={<MetadataPage><AuthPage register /></MetadataPage>} /><Route path="/welcome" element={<MetadataPage><LandingPage /></MetadataPage>} /></CustomRoutes></Admin></ConfigContext.Provider>;
}
