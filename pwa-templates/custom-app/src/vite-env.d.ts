/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_BASE_URL: string
  readonly VITE_API_URL?: string
  readonly VITE_S3_PUBLIC_DOMAIN: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<{}, {}, any>
  export default component
}

declare global {
  interface Window {
    getDeviceId?: () => string;
    trackPWAEvent?: (eventType: string) => void;
    subscribeToPush?: (vapidKey?: string) => Promise<PushSubscription | null>;
    installPWA?: () => void;
    scheduleReturnFlowRiskCancelPrompt?: (reason?: string) => boolean;
  }
}
