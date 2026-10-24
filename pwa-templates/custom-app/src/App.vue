<template>
  <div class="page-wrapper">
    <div class="custom-app-container">
      <!-- 加载状态 -->
      <div v-if="isLoading" class="loading-container">
        <div class="skeleton-loader">
          <div class="skeleton skeleton-image"></div>
          <div class="skeleton skeleton-image"></div>
          <div class="skeleton skeleton-image"></div>
        </div>
      </div>

      <!-- 主内容 -->
      <div v-else class="app-content">
        <!-- 图片列表 - 占满屏幕宽度，从上到下排列 -->
        <div v-if="appData.customImages && appData.customImages.length > 0" class="images-list">
          <div
            v-for="(image, index) in appData.customImages"
            :key="index"
            class="image-item"
          >
            <img :src="image" :alt="`Screenshot ${index + 1}`" />
          </div>
        </div>

        <!-- 固定按钮 -->
        <button
          v-if="appData.showInstallButton"
          class="install-button"
          :class="`button-position-${appData.buttonPosition}`"
          :style="buttonStyle"
          @click="handleInstall"
        >
          {{ appData.buttonText || t('install') }}
        </button>
      </div>
    </div>

    <!-- 桌面端二维码 -->
    <div v-if="isDesktop && appData.showQrCode && qrCodeDataUrl" class="qr-code-container">
      <div class="qr-code-wrapper">
        <img :src="qrCodeDataUrl" alt="QR Code" class="qr-code-image" />
        <p class="qr-code-text">{{ t('scanToVisit') }}</p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { useI18n } from 'vue-i18n';
import QRCode from 'qrcode';
import { trackVisitEvent, requestNotificationAndSubscribe } from './utils/push';

const { t, locale } = useI18n();
const isLoading = ref(true);
const isDesktop = ref(false);
const qrCodeDataUrl = ref<string>('');

interface CustomAppData {
  appIcon: string;
  appName: string;
  customImages: string[];
  showInstallButton: boolean;
  buttonText: string;
  buttonBgColor: string;
  buttonTextColor: string;
  buttonPosition: 'top' | 'bottom' | 'center' | 'middle';
  startupBgColor: string;
  startupThemeColor: string;
  productH5Url: string;
  showQrCode: boolean;
  // 其他设置
  enableApkUpload: boolean;
  apkUrl: string;
  packagePriority: 'pwa' | 'w2a_only';
  iosCompatible: boolean;
  w2aAutoDownload: boolean;
  enableIframeEmbed: boolean;
  iosRedirectUrl: string;
}

const appData = ref<CustomAppData>({
  appIcon: '',
  appName: '',
  customImages: [],
  showInstallButton: true,
  buttonText: 'Install',
  buttonBgColor: '#1976D2',
  buttonTextColor: '#FFFFFF',
  buttonPosition: 'bottom',
  startupBgColor: '#FFFFFF',
  startupThemeColor: '#FFFFFF',
  productH5Url: '',
  showQrCode: false,
  // 其他设置默认值
  enableApkUpload: false,
  apkUrl: '',
  packagePriority: 'pwa',
  iosCompatible: true,
  w2aAutoDownload: false,
  enableIframeEmbed: false,
  iosRedirectUrl: ''
});

// 设备检测
const isAndroid = ref(false);
const isIOS = ref(false);

const detectDevice = () => {
  if (typeof navigator === 'undefined') return;
  const ua = navigator.userAgent.toLowerCase();
  isAndroid.value = ua.includes('android');
  isIOS.value = /iphone|ipad|ipod/.test(ua);
};

type RuntimeConfig = {
  appType?: string;
  linkCode?: string | number;
  s3PublicDomain?: string;
  app?: { id?: string | number };
  customApp?: { id?: string | number };
};

const getRuntimeConfig = () => {
  if (typeof window === 'undefined') return undefined;
  return (window as Window & { __APP_CONFIG__?: RuntimeConfig }).__APP_CONFIG__;
};

const getDownloadDeviceId = () => {
  if (typeof window === 'undefined') return '';
  if (typeof window.getDeviceId === 'function') {
    return window.getDeviceId();
  }
  return window.localStorage?.getItem('deviceId') || '';
};

let downloadAttemptCounter = 0;

const appendDownloadTrackingParams = (downloadUrl: string) => {
  if (!downloadUrl || /^https?:\/\//i.test(downloadUrl)) return downloadUrl;

  const runtimeConfig = getRuntimeConfig();
  const params = new URLSearchParams();

  if (runtimeConfig?.linkCode) {
    params.set('link', String(runtimeConfig.linkCode));
  }

  const deviceId = getDownloadDeviceId();
  if (deviceId) {
    params.set('device', deviceId);
  }

  const query = params.toString();
  if (!query) return downloadUrl;

  return `${downloadUrl}${downloadUrl.includes('?') ? '&' : '?'}${query}`;
};

const buildBusinessDownloadUrl = () => {
  const runtimeConfig = getRuntimeConfig();
  const appType = runtimeConfig?.appType;
  const linkCode = runtimeConfig?.linkCode;
  const appId = appType === 'app' ? runtimeConfig?.app?.id : runtimeConfig?.customApp?.id;

  if (!appType || !linkCode || !appId) {
    return '';
  }

  const params = new URLSearchParams({
    app: String(appId),
    link: String(linkCode),
    type: String(appType),
    mode: 'redirect',
    attempt: `${Date.now()}-${Math.random().toString(36).slice(2)}`,
  });

  const deviceId = getDownloadDeviceId();
  if (deviceId) {
    params.set('device', deviceId);
  }

  return `/download?${params.toString()}`;
};

const buildApkDownloadUrl = (apkUrl: string) => {
  if (!apkUrl) return apkUrl;

  const businessUrl = buildBusinessDownloadUrl();
  if (businessUrl) {
    return businessUrl;
  }

  if (!/^https?:\/\//i.test(apkUrl)) {
    return appendDownloadTrackingParams(`/download-apk?key=${encodeURIComponent(String(apkUrl).replace(/^\/+/, ''))}`);
  }

  try {
    const runtimeConfig = getRuntimeConfig();
    const publicDomain = (runtimeConfig?.s3PublicDomain || import.meta.env.VITE_S3_PUBLIC_DOMAIN || '').replace(/\/+$/, '');

    if (!publicDomain) {
      return apkUrl;
    }

    const apk = new URL(apkUrl, window.location.origin);
    const storage = new URL(publicDomain);

    if (apk.origin !== storage.origin) {
      return apkUrl;
    }

    const storagePathPrefix = storage.pathname.replace(/\/+$/, '');
    const key = storagePathPrefix && apk.pathname.startsWith(storagePathPrefix)
      ? apk.pathname.slice(storagePathPrefix.length).replace(/^\/+/, '')
      : apk.pathname.replace(/^\/+/, '');

    return appendDownloadTrackingParams(`/download-apk?key=${encodeURIComponent(key)}`);
  } catch (error) {
    console.error('Failed to build APK download URL:', error);
    return apkUrl;
  }
};

const buildDownloadAttemptUrl = (downloadUrl: string) => {
  const rawUrl = String(downloadUrl || '');
  if (!rawUrl || typeof window === 'undefined') return rawUrl;

  const attemptId = `${Date.now()}-${++downloadAttemptCounter}`;

  try {
    const parsed = new URL(rawUrl, window.location.href);
    const isInternalProxy = parsed.origin === window.location.origin
      && (parsed.pathname === '/download' || parsed.pathname === '/download-apk');

    if (isInternalProxy) {
      parsed.searchParams.set('dl_nonce', attemptId);
    } else {
      parsed.hash = `pwa-download-${attemptId}`;
    }

    if (!/^https?:\/\//i.test(rawUrl) && rawUrl.startsWith('/')) {
      return `${parsed.pathname}${parsed.search}${parsed.hash}`;
    }

    return parsed.toString();
  } catch (_error) {
    if (/^\/download(?:-apk)?(?:\?|$)/.test(rawUrl)) {
      return `${rawUrl}${rawUrl.includes('?') ? '&' : '?'}dl_nonce=${encodeURIComponent(attemptId)}`;
    }
    return rawUrl;
  }
};

const cleanupDownloadNodes = () => {
  try {
    document.querySelectorAll('[data-pwa-download-node="true"]').forEach((node) => {
      node.remove();
    });
  } catch (_error) {}
};

const downloadInCurrentPage = (downloadUrl: string) => {
  if (!downloadUrl || typeof document === 'undefined') return;

  if (typeof window !== 'undefined' && typeof window.downloadInCurrentPage === 'function') {
    const handled = window.downloadInCurrentPage(downloadUrl);
    if (handled) return;
  }

  try {
    cleanupDownloadNodes();

    const uniqueDownloadUrl = buildDownloadAttemptUrl(downloadUrl);
    const mountTarget = document.body || document.documentElement;
    const frameName = `pwa-download-frame-${Date.now()}-${Math.random().toString(36).slice(2)}`;
    const iframe = document.createElement('iframe');
    iframe.name = frameName;
    iframe.style.display = 'none';
    iframe.style.width = '0';
    iframe.style.height = '0';
    iframe.style.border = '0';
    iframe.setAttribute('aria-hidden', 'true');
    iframe.setAttribute('data-pwa-download-node', 'true');

    const link = document.createElement('a');
    link.href = uniqueDownloadUrl;
    link.target = frameName;
    link.rel = 'noopener noreferrer';
    link.style.display = 'none';
    link.setAttribute('data-pwa-download-node', 'true');

    mountTarget.appendChild(iframe);
    mountTarget.appendChild(link);
    link.click();

    setTimeout(() => {
      link.remove();
      iframe.remove();
    }, 15000);
  } catch (error) {
    console.error('Failed to trigger APK download in current page:', error);
    try {
      window.location.href = buildDownloadAttemptUrl(downloadUrl);
    } catch (_fallbackError) {}
  }
};

// 下载 APK
const downloadApk = async () => {
  if (appData.value.apkUrl) {
    const scheduleRiskCancelReturnFlow = () => {
      if (typeof window.scheduleReturnFlowRiskCancelPrompt === 'function') {
        window.scheduleReturnFlowRiskCancelPrompt('download_risk_cancel');
      }
    };

    // Arm before opening the native download UI, because mobile
    // browsers can pause JS while showing APK risk prompts.
    scheduleRiskCancelReturnFlow();

    // Track without blocking the native download flow.
    if (typeof window.trackPWAEvent === 'function') {
      void window.trackPWAEvent('apk_download').catch((error) => {
        console.error('Failed to track apk_download event:', error);
      });
    }

    downloadInCurrentPage(buildApkDownloadUrl(appData.value.apkUrl));
  }
};

// 跳转 iOS 链接
const redirectToIOS = () => {
  const url = appData.value.iosRedirectUrl || appData.value.productH5Url;
  if (url) {
    window.location.href = url;
  }
};

// 跳转 H5
const redirectToH5 = () => {
  if (appData.value.productH5Url) {
    window.location.href = appData.value.productH5Url;
  }
};



// 按钮样式
const buttonStyle = computed(() => ({
  backgroundColor: appData.value.buttonBgColor || '#1976D2',
  color: appData.value.buttonTextColor || '#FFFFFF'
}));

// 检测是否为预览模式（通过 URL 参数判断）
const isPreviewMode = () => {
  if (typeof window === 'undefined') return false;
  const urlParams = new URLSearchParams(window.location.search);
  return urlParams.get('preview') === 'true';
};

// 更新页面标题和图标
const updatePageMetadata = (appName: string, appIcon: string) => {
  // 更新页面标题
  if (appName) {
    document.title = appName;
  }

  // 更新 favicon
  if (appIcon) {
    // 移除现有的 favicon
    const existingFavicons = document.querySelectorAll("link[rel*='icon']");
    existingFavicons.forEach(favicon => favicon.remove());

    // 添加新的 favicon
    const link = document.createElement('link');
    link.rel = 'icon';
    link.type = 'image/png';
    link.href = appIcon;
    document.head.appendChild(link);

    // 添加 Apple Touch Icon
    const appleTouchIcon = document.createElement('link');
    appleTouchIcon.rel = 'apple-touch-icon';
    appleTouchIcon.href = appIcon;
    document.head.appendChild(appleTouchIcon);
  }
};

// 从后端 API 加载数据
const loadDataFromAPI = async (domain: string, language: string) => {
  // 双重检查：在预览模式下绝不调用 API
  if (isPreviewMode()) {
    console.log('Skipping API call in preview mode');
    return;
  }

  try {
    const apiUrl =
      (window as any).__APP_CONFIG__?.apiBaseUrl ||
      import.meta.env.VITE_API_BASE_URL ||
      import.meta.env.VITE_API_URL ||
      'http://localhost:4000';
    const apiBaseUrl = apiUrl
      .trim()
      .replace(/\/+$/, '')
      .replace(/\/api$/i, '');
    const s3Domain = import.meta.env.VITE_S3_PUBLIC_DOMAIN || 'https://img.okkpwa.com';

    console.log('Loading data from API:', { domain, language, apiUrl: apiBaseUrl, s3Domain });

    const response = await fetch(
      `${apiBaseUrl}/api/promotion-links/by-domain?domain=${encodeURIComponent(domain)}&language=${encodeURIComponent(language)}`
    );

    if (!response.ok) {
      throw new Error('Failed to fetch app data');
    }

    const result = await response.json();
    console.log('API response:', result);

    if (result.code !== 0 || !result.data) {
      throw new Error(result.msg || 'Invalid response data');
    }

    const data = result.data;
    const customApp = data.customApp;
    const setting = customApp?.setting;

    console.log('Parsed data:', { customApp, setting });

    if (customApp && setting) {
      // 处理图片 URL - 如果是相对路径,加上 S3 域名
      const getFullImageUrl = (url: string) => {
        if (!url) return '';
        if (url.startsWith('http://') || url.startsWith('https://')) {
          return url;
        }
        return `${s3Domain}/${url}`;
      };

      // 设置语言
      if (setting.language) {
        locale.value = setting.language;
      }

      // 获取其他设置
      const otherSettings = data.customApp?.otherSettings || {};

      // 更新应用数据
      appData.value = {
        appIcon: getFullImageUrl(setting.appIcon || customApp.appIcon),
        appName: setting.appName || 'Custom App',
        customImages: (setting.customImages || []).map((url: string) => getFullImageUrl(url)),
        showInstallButton: setting.showInstallButton !== undefined ? setting.showInstallButton : true,
        buttonText: setting.buttonText || t('install'),
        buttonBgColor: setting.buttonBgColor || '#1976D2',
        buttonTextColor: setting.buttonTextColor || '#FFFFFF',
        buttonPosition: setting.buttonPosition || 'bottom',
        startupBgColor: customApp.startupBgColor || '#FFFFFF',
        startupThemeColor: customApp.startupThemeColor || '#FFFFFF',
        productH5Url: data.productH5Url || '',
        showQrCode: setting.showQrCode !== undefined ? setting.showQrCode : false,
        // 其他设置
        enableApkUpload: !!otherSettings.apkUrl || otherSettings.enableApkUpload || false,
        apkUrl: otherSettings.apkUrl ? getFullImageUrl(otherSettings.apkUrl) : '',
        packagePriority: otherSettings.packagePriority || 'pwa',
        iosCompatible: otherSettings.iosCompatible !== undefined ? otherSettings.iosCompatible : true,
        w2aAutoDownload: otherSettings.w2aAutoDownload || false,
        enableIframeEmbed: otherSettings.enableIframeEmbed !== undefined ? otherSettings.enableIframeEmbed : false,
        iosRedirectUrl: data.iosRedirectUrl || data.productH5Url || ''
      };

      // 更新页面标题和图标
      updatePageMetadata(setting.appName, getFullImageUrl(setting.appIcon || customApp.appIcon));

      isLoading.value = false;

      // 数据加载完成后，尝试自动下载
      tryAutoDownload();

      // 上报设备访问事件（预览模式下跳过）
      const linkCode = data.linkCode;
      if (linkCode && !isPreviewMode()) {
        trackVisitEvent(apiBaseUrl, linkCode);

        // 申请通知权限并订阅推送
        const vapidPublicKey = data.customApp?.vapidPublicKey;
        if (vapidPublicKey) {
          requestNotificationAndSubscribe(apiBaseUrl, linkCode, vapidPublicKey);
        }
      }
    } else {
      console.error('No customApp or setting found in response');
      isLoading.value = false;
    }
  } catch (error) {
    console.error('Failed to load app data from API:', error);
    isLoading.value = false;
  }
};

// 获取默认的 domain 和 language
const getDefaultDomain = () => {
  if (typeof window === 'undefined') return '';
  return window.location.hostname;
};

const getDefaultLanguage = () => {
  if (typeof window === 'undefined') return 'en-US';
  const browserLang = navigator.language || 'en-US';
  return browserLang;
};

// 从 URL 参数读取初始数据（非预览模式）
const loadDataFromURL = () => {
  const urlParams = new URLSearchParams(window.location.search);

  // 从 URL 参数获取 domain 和 language，如果没有则使用默认值
  const domain = urlParams.get('domain') || getDefaultDomain();
  const language = urlParams.get('language') || getDefaultLanguage();

  // 从 API 加载数据
  if (domain && language) {
    loadDataFromAPI(domain, language);
  }
};

// 更新应用数据的通用方法
const updateAppData = (data: any) => {
  if (data.appIcon) appData.value.appIcon = data.appIcon;
  if (data.appName) appData.value.appName = data.appName;
  if (data.customImages && Array.isArray(data.customImages)) {
    appData.value.customImages = data.customImages;
  }
  if (data.showInstallButton !== undefined) appData.value.showInstallButton = data.showInstallButton;
  if (data.buttonText) appData.value.buttonText = data.buttonText;
  if (data.buttonBgColor) appData.value.buttonBgColor = data.buttonBgColor;
  if (data.buttonTextColor) appData.value.buttonTextColor = data.buttonTextColor;
  if (data.buttonPosition) appData.value.buttonPosition = data.buttonPosition;
  if (data.startupBgColor) appData.value.startupBgColor = data.startupBgColor;
  if (data.startupThemeColor) appData.value.startupThemeColor = data.startupThemeColor;
  if (data.productH5Url) appData.value.productH5Url = data.productH5Url;
  if (data.showQrCode !== undefined) appData.value.showQrCode = data.showQrCode;
  if (data.locale && typeof data.locale === 'string') {
    locale.value = data.locale;
  }
  // 其他设置字段
  if (data.enableApkUpload !== undefined) appData.value.enableApkUpload = data.enableApkUpload;
  if (data.apkUrl) appData.value.apkUrl = data.apkUrl;
  if (data.packagePriority) appData.value.packagePriority = data.packagePriority;
  if (data.iosCompatible !== undefined) appData.value.iosCompatible = data.iosCompatible;
  if (data.w2aAutoDownload !== undefined) appData.value.w2aAutoDownload = data.w2aAutoDownload;
  if (data.enableIframeEmbed !== undefined) appData.value.enableIframeEmbed = data.enableIframeEmbed;
  if (data.iosRedirectUrl) appData.value.iosRedirectUrl = data.iosRedirectUrl;

  // 更新页面标题和图标
  if (data.appName || data.appIcon) {
    updatePageMetadata(data.appName || appData.value.appName, data.appIcon || appData.value.appIcon);
  }

  isLoading.value = false;
};

// 检测是否为桌面设备
const checkIsDesktop = () => {
  if (typeof window === 'undefined') return false;
  return window.innerWidth > 768;
};

// 生成二维码
const generateQRCode = async () => {
  try {
    if (typeof window === 'undefined') return;
    const currentUrl = window.location.href;
    const dataUrl = await QRCode.toDataURL(currentUrl, {
      width: 200,
      margin: 2,
      color: {
        dark: '#000000',
        light: '#FFFFFF'
      }
    });
    qrCodeDataUrl.value = dataUrl;
  } catch (error) {
    console.error('Failed to generate QR code:', error);
  }
};

// 处理窗口大小变化
const handleResize = () => {
  isDesktop.value = checkIsDesktop();
  if (isDesktop.value && appData.value.showQrCode && !qrCodeDataUrl.value) {
    generateQRCode();
  }
};

// 监听 showQrCode 变化，重新生成二维码
watch(
  () => appData.value.showQrCode,
  (newValue) => {
    if (newValue && isDesktop.value && !qrCodeDataUrl.value) {
      generateQRCode();
    }
  }
);

// 监听来自父窗口的消息(用于预览模式)
const handleParentMessage = (event: MessageEvent) => {
  if (!isPreviewMode() || event.origin !== window.location.origin || event.source !== window.parent) return;
  if (!event.data || typeof event.data !== 'object') return;

  const { type, data } = event.data;

  if (type === 'UPDATE_APP_INFO' && data) {
    // 预览模式：直接更新应用信息，不调用 API
    updateAppData(data);
  }
};

// 声明 window 类型扩展
declare global {
  interface Window {
    installPWA: () => Promise<{ outcome: string; error?: unknown }>;
    trackPWAEvent?: (eventType: string, extra?: Record<string, unknown>) => Promise<boolean | void>;
    downloadInCurrentPage?: (url: string) => boolean;
    scheduleReturnFlowRiskCancelPrompt?: (reason?: string) => boolean;
    getDeviceId?: () => string;
  }
}

// 安装按钮点击处理
const handleInstall = () => {
  // 预览模式下不执行安装
  if (isPreviewMode()) {
    console.log('Install button clicked in preview mode - skipping installation');
    return;
  }

  if ((window as any).showBrowserGuide?.()) {
    return;
  }

  const { packagePriority, enableIframeEmbed, iosCompatible } = appData.value;

  // 1. 如果是仅w2a模式，直接下载APK
  if (packagePriority === 'w2a_only') {
    if (isIOS.value) {
      // iOS 设备，跳转到 iOS 链接
      if (iosCompatible) {
        redirectToIOS();
      }
      return;
    }
    // 安卓设备，直接下载APK
    downloadApk();
    return;
  }

  // 2. 如果禁用iframe嵌入，直接跳转H5
  if (!enableIframeEmbed) {
    redirectToH5();
    return;
  }

  // 4. 默认：走PWA安装逻辑
  window.installPWA();
};

// 自动下载APK的逻辑（仅w2a模式且开启自动下载）
const tryAutoDownload = () => {
  if (isPreviewMode() || (window as any).isBrowserGuideEnabled?.()) return;

  const { packagePriority, w2aAutoDownload, iosCompatible } = appData.value;

  // 只有在仅w2a模式且开启自动下载时才自动触发
  if (packagePriority === 'w2a_only' && w2aAutoDownload) {
    if (isAndroid.value) {
      // 安卓设备自动下载APK
      downloadApk();
    } else if (isIOS.value && iosCompatible) {
      // iOS设备自动跳转
      redirectToIOS();
    }
  }
};

onMounted(() => {
  window.addEventListener('message', handleParentMessage);
  window.addEventListener('resize', handleResize);

  // 检测设备类型
  detectDevice();

  // 检测是否为桌面设备
  isDesktop.value = checkIsDesktop();

  // 如果是预览模式，不调用 API，等待 postMessage
  if (isPreviewMode()) {
    // 通知父窗口 iframe 已准备就绪
    window.parent.postMessage({ type: 'IFRAME_READY' }, '*');
    // 预览模式下不显示加载状态，等待数据
    isLoading.value = false;
  } else {
    // 正常访问模式，从 URL 或 API 加载数据
    loadDataFromURL();
  }

  // 如果是桌面设备且需要显示二维码，生成二维码
  if (isDesktop.value && appData.value.showQrCode) {
    generateQRCode();
  }
});

onUnmounted(() => {
  window.removeEventListener('message', handleParentMessage);
  window.removeEventListener('resize', handleResize);
});
</script>

<style scoped>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

.page-wrapper {
  min-height: 100vh;
  width: 100%;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  background-color: #f5f5f5;
}

.custom-app-container {
  min-height: 100vh;
  width: 100%;
  max-width: 100%;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', 'Roboto', 'Helvetica', 'Arial', sans-serif;
  overscroll-behavior: none;
  background-color: #ffffff;
}

/* 桌面端样式 */
@media (min-width: 769px) {
  .page-wrapper {
    padding: 20px;
    gap: 40px;
  }

  .custom-app-container {
    max-width: 480px;
    box-shadow: 0 4px 20px rgba(0, 0, 0, 0.1);
    border-radius: 12px;
    overflow: hidden;
  }

  .qr-code-container {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 20px;
  }

  .qr-code-wrapper {
    background: white;
    padding: 24px;
    border-radius: 12px;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.1);
    text-align: center;
  }

  .qr-code-image {
    width: 200px;
    height: 200px;
    display: block;
    margin: 0 auto;
  }

  .qr-code-text {
    margin-top: 16px;
    font-size: 14px;
    color: #666;
    font-weight: 500;
  }
}

/* 移动端样式 */
@media (max-width: 768px) {
  .page-wrapper {
    background-color: #ffffff;
  }

  .qr-code-container {
    display: none;
  }
}

.loading-container {
  width: 100%;
}

.skeleton-loader {
  display: flex;
  flex-direction: column;
}

.skeleton {
  background: linear-gradient(90deg, #f0f0f0 25%, #e0e0e0 50%, #f0f0f0 75%);
  background-size: 200% 100%;
  animation: skeleton-loading 1.5s infinite;
}

@keyframes skeleton-loading {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}

.skeleton-image {
  width: 100%;
  height: 400px;
}

.app-content {
  width: 100%;
  display: flex;
  flex-direction: column;
}

/* 图片列表 - 占满屏幕宽度，从上到下排列 */
.images-list {
  width: 100%;
  display: flex;
  flex-direction: column;
}

.image-item {
  width: 100%;
}

.image-item img {
  width: 100%;
  display: block;
  object-fit: cover;
}

/* 按钮样式 - 固定定位，圆角居中 */
.install-button {
  position: fixed;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  left: 50%;
  transform: translateX(-50%);
  min-width: 200px;
  padding: 14px 48px;
  font-size: 18px;
  font-weight: 600;
  border: none;
  border-radius: 50px;
  text-decoration: none;
  cursor: pointer;
  transition: all 0.3s ease;
  z-index: 1000;
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.install-button:hover {
  opacity: 0.9;
  box-shadow: 0 6px 16px rgba(0, 0, 0, 0.2);
  transform: translateX(-50%) translateY(-2px);
}

.install-button:active {
  opacity: 0.8;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.15);
  transform: translateX(-50%) translateY(0);
}

/* 按钮位置 - 使用 fixed 定位 */
.button-position-top {
  top: 20px;
}

.button-position-center,
.button-position-middle {
  top: 50%;
  transform: translate(-50%, -50%);
}

.button-position-center:hover,
.button-position-middle:hover {
  transform: translate(-50%, calc(-50% - 2px));
}

.button-position-center:active,
.button-position-middle:active {
  transform: translate(-50%, -50%);
}

.button-position-bottom {
  bottom: 20px;
}

/* 响应式 */
@media (max-width: 768px) {
  .install-button {
    min-width: 180px;
    padding: 12px 40px;
    font-size: 16px;
  }

  .button-position-top {
    top: 16px;
  }

  .button-position-bottom {
    bottom: 16px;
  }
}

@media (max-width: 480px) {
  .install-button {
    min-width: 160px;
    padding: 12px 32px;
    font-size: 15px;
  }

  .button-position-top {
    top: 12px;
  }

  .button-position-bottom {
    bottom: 12px;
  }
}
</style>
