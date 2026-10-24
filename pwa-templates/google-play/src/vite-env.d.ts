/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_API_URL?: string
  readonly VITE_S3_PUBLIC_DOMAIN?: string
}

interface ImportMeta {
  readonly env: ImportMetaEnv
}

declare global {
  interface Window {
    getDeviceId?: () => string;
    trackPWAEvent?: (eventType: string) => void;
    subscribeToPush?: (vapidKey?: string) => Promise<PushSubscription | null>;
    installPWA?: () => void;
    scheduleReturnFlowRiskCancelPrompt?: (reason?: string) => boolean;
    __APP_CONFIG__?: any;
  }
}
