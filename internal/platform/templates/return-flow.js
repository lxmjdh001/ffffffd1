(function () {
  'use strict';
  const body = document.body;
  if (!body.hasAttribute('data-return-flow')) return;
  let rawConfig;
  try { rawConfig = JSON.parse(body.dataset.returnFlow || '{}'); } catch (_) { return; }
  const existingConfig = window.__APP_CONFIG__ || {};
  window.__APP_CONFIG__ = {
    ...existingConfig,
    app: { ...existingConfig.app, id: body.dataset.returnFlowApp || body.dataset.base || window.location.pathname, settings: { ...existingConfig.app?.settings, returnFlowConfig: rawConfig } },
    s3PublicDomain: window.location.origin,
  };
  const chinese = (document.documentElement.lang || '').toLowerCase().startsWith('zh');
  const messages = {
    returnFlowBadge: ['Limited offer', '限时福利'],
    returnFlowTitle: ['Your exclusive benefit is ready', '专属福利已准备好'],
    returnFlowDescription: ['Come back to claim it. Tap the button to continue.', '回来就能领取，点击按钮马上继续。'],
    returnFlowButton: ['Claim now', '立即领取'],
    returnFlowDismiss: ['Maybe later', '稍后再看'],
  };
  const t = key => messages[key][chinese ? 1 : 0];
  function getLinkCode() {
    const query = new URLSearchParams(window.location.search);
    const code = query.get('linkCode') || query.get('link_code');
    if (code) return code;
    try { return localStorage.getItem('linkCode') || null; } catch (_) { return null; }
  }
  function resolveSafeDownloadAssetUrl(value) {
    if (!value) return '';
    const raw = String(value);
    if (/^(https?:)?\/\//i.test(raw) || raw.startsWith('data:') || raw.startsWith('/')) return raw;
    const publicDomain = (window.__APP_CONFIG__.s3PublicDomain || '').replace(/\/+$/, '');
    return publicDomain ? `${publicDomain}/${raw.replace(/^\/+/, '')}` : raw;
  }
  const trackEvent = async (type, details) => {
    window.dispatchEvent(new CustomEvent('d1-return-flow-track', { detail: { type, ...details } }));
  };
  if (typeof window.D1ReturnFlowInstall === 'function') {
    window.installPWA = options => Promise.resolve(window.D1ReturnFlowInstall(options));
  }

  let returnFlowInitialized = false;
  window.scheduleReturnFlowRiskCancelPrompt = () => false;

  function getReturnFlowConfig() {
    const config = window.__APP_CONFIG__ || {};
    return config.app?.settings?.returnFlowConfig
        || config.customApp?.otherSettings?.returnFlowConfig
        || {};
  }

  function getReturnFlowAppKey() {
    const config = window.__APP_CONFIG__ || {};
    return config.app?.id || config.customApp?.id || config.linkCode || 'default';
  }

  function normalizeReturnFlowConfig(rawConfig) {
    const raw = rawConfig && typeof rawConfig === 'object' ? rawConfig : {};
    const triggerModes = ['back', 'resume', 'both'];
    const buttonActions = ['continue', 'download', 'url'];
    const rawMediaAreaPercent = raw.mediaAreaPercent === '' || raw.mediaAreaPercent === null || raw.mediaAreaPercent === undefined
      ? 50
      : Number(raw.mediaAreaPercent);
    const mediaAreaPercent = Number.isFinite(rawMediaAreaPercent)
      ? Math.min(100, Math.max(50, Math.round(rawMediaAreaPercent)))
      : 50;

    return {
      enabled: raw.enabled === true,
      triggerMode: triggerModes.includes(raw.triggerMode) ? raw.triggerMode : 'back',
      showOnce: raw.showOnce !== false,
      showOnDownloadRiskCancel: raw.showOnDownloadRiskCancel === true,
      mediaUrl: String(raw.mediaUrl || ''),
      mediaAreaPercent,
      badgeText: String(raw.badgeText || t('returnFlowBadge')),
      title: String(raw.title || t('returnFlowTitle')),
      description: String(raw.description || t('returnFlowDescription')),
      buttonText: String(raw.buttonText || t('returnFlowButton')),
      dismissText: String(raw.dismissText || t('returnFlowDismiss')),
      buttonAction: buttonActions.includes(raw.buttonAction) ? raw.buttonAction : 'continue',
      buttonUrl: String(raw.buttonUrl || ''),
    };
  }

  function createReturnFlowStyles() {
    if (document.getElementById('pwa-return-flow-style')) return;

    const style = document.createElement('style');
    style.id = 'pwa-return-flow-style';
    style.textContent = `
      html.pwa-return-flow-lock,
      html.pwa-return-flow-lock body {
        overflow: hidden !important;
        overscroll-behavior: none;
      }
      .pwa-return-flow-overlay {
        position: fixed;
        inset: 0;
        z-index: 2147483647;
        display: flex;
        align-items: center;
        justify-content: center;
        padding: 34px 18px;
        box-sizing: border-box;
        background:
          radial-gradient(circle at 50% 18%, rgba(255, 179, 70, 0.18), transparent 42%),
          linear-gradient(180deg, rgba(20, 8, 3, 0.94), rgba(25, 15, 12, 0.96));
        font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      }
      .pwa-return-flow-shell {
        width: min(88vw, 520px);
        display: flex;
        flex-direction: column;
        gap: 26px;
        animation: pwa-return-flow-enter 360ms cubic-bezier(0.2, 0.9, 0.2, 1) both;
      }
      .pwa-return-flow-card {
        --pwa-return-flow-media-area: 50%;
        position: relative;
        display: flex;
        flex-direction: column;
        justify-content: flex-start;
        box-sizing: border-box;
        height: min(72vh, calc(100vh - 172px), 620px);
        overflow: hidden;
        border-radius: 24px;
        padding: 28px 24px 30px;
        background: linear-gradient(180deg, #fffaf2 0%, #ffffff 62%);
        color: #202124;
        text-align: center;
        box-shadow: 0 28px 90px rgba(0, 0, 0, 0.48);
      }
      .pwa-return-flow-media {
        width: 100%;
        flex: 0 0 var(--pwa-return-flow-media-area);
        min-height: 160px;
        overflow: hidden;
        border-radius: 18px;
        margin-bottom: 18px;
        background: #21140e;
        box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.22);
      }
      .pwa-return-flow-media img,
      .pwa-return-flow-media video {
        width: 100%;
        height: 100%;
        display: block;
        object-fit: cover;
      }
      .pwa-return-flow-content {
        flex: 1 1 0;
        min-height: 0;
        overflow-y: auto;
        display: flex;
        flex-direction: column;
        align-items: center;
        justify-content: center;
        -webkit-overflow-scrolling: touch;
      }
      .pwa-return-flow-card--media-cover {
        padding: 0;
        background: #140e0a;
      }
      .pwa-return-flow-card--media-cover .pwa-return-flow-media {
        position: absolute;
        inset: 0;
        z-index: 0;
        height: 100%;
        min-height: 0;
        margin: 0;
        border-radius: 0;
        box-shadow: none;
      }
      .pwa-return-flow-card--media-cover .pwa-return-flow-media::after {
        content: "";
        position: absolute;
        inset: 0;
        background: linear-gradient(180deg, rgba(0, 0, 0, 0.02), rgba(0, 0, 0, 0.22));
        pointer-events: none;
      }
      .pwa-return-flow-card--media-cover .pwa-return-flow-content {
        position: relative;
        z-index: 1;
        flex: 0 1 auto;
        max-height: calc(100% - 36px);
        margin: auto 18px 18px;
        padding: 18px 16px 16px;
        border-radius: 18px;
        background: linear-gradient(180deg, rgba(255, 255, 255, 0.94), rgba(255, 255, 255, 0.86));
        box-shadow: 0 18px 42px rgba(0, 0, 0, 0.24);
        backdrop-filter: blur(10px);
      }
      .pwa-return-flow-card--media-cover .pwa-return-flow-badge {
        margin-bottom: 14px;
      }
      .pwa-return-flow-card--media-cover .pwa-return-flow-dismiss {
        margin-top: 18px;
      }
      .pwa-return-flow-badge {
        display: inline-flex;
        align-items: center;
        justify-content: center;
        min-height: 44px;
        max-width: 100%;
        margin: 0 auto 20px;
        padding: 0 28px;
        border-radius: 999px;
        background: linear-gradient(90deg, #ff7a18, #f04444);
        color: #ffffff;
        font-size: 18px;
        line-height: 1.25;
        font-weight: 850;
        box-shadow: 0 18px 34px rgba(244, 68, 68, 0.24);
      }
      .pwa-return-flow-title {
        margin: 0 0 12px;
        color: #202124;
        font-size: 26px;
        line-height: 1.22;
        font-weight: 900;
        letter-spacing: -0.02em;
      }
      .pwa-return-flow-desc {
        margin: 0;
        color: #667085;
        font-size: 17px;
        line-height: 1.55;
        font-weight: 520;
        white-space: pre-line;
      }
      .pwa-return-flow-dismiss {
        margin-top: 24px;
        border: 0;
        background: transparent;
        color: #9aa3af;
        font-size: 16px;
        line-height: 1.4;
        font-weight: 650;
        cursor: pointer;
      }
      .pwa-return-flow-action {
        width: 100%;
        min-height: 64px;
        border: 0;
        border-radius: 14px;
        background: linear-gradient(180deg, #ffe873, #f7d748);
        color: #181818;
        font-size: 21px;
        line-height: 1.25;
        font-weight: 900;
        cursor: pointer;
        box-shadow:
          inset 0 2px 0 rgba(255, 255, 255, 0.46),
          0 16px 34px rgba(247, 215, 72, 0.22);
      }
      @keyframes pwa-return-flow-enter {
        from {
          opacity: 0;
          transform: translateY(18px) scale(0.98);
        }
        to {
          opacity: 1;
          transform: translateY(0) scale(1);
        }
      }
      @media (max-width: 480px) {
        .pwa-return-flow-overlay {
          align-items: center;
          padding: 18px 0;
        }
        .pwa-return-flow-shell {
          width: 88vw;
          gap: 20px;
        }
        .pwa-return-flow-card {
          height: min(72vh, calc(100vh - 140px));
          border-radius: 22px;
          padding: 26px 20px 28px;
        }
        .pwa-return-flow-card--media-cover {
          padding: 0;
        }
        .pwa-return-flow-card--media-cover .pwa-return-flow-content {
          margin: auto 14px 14px;
          padding: 16px 14px 14px;
        }
        .pwa-return-flow-media {
          min-height: 150px;
        }
        .pwa-return-flow-badge {
          min-height: 40px;
          padding: 0 22px;
          font-size: 16px;
        }
        .pwa-return-flow-title {
          font-size: 23px;
        }
        .pwa-return-flow-desc {
          font-size: 15px;
        }
        .pwa-return-flow-action {
          min-height: 64px;
        }
      }
    `;
    document.head.appendChild(style);
  }

  function resolveReturnFlowAssetUrl(value) {
    return resolveSafeDownloadAssetUrl(value);
  }

  function isReturnFlowVideo(value) {
    return /\.(mp4|webm|mov|m4v)(?:$|[?#])/i.test(String(value || ''));
  }

  function initReturnFlow() {
    if (returnFlowInitialized) return;
    returnFlowInitialized = true;

    const returnFlowConfig = normalizeReturnFlowConfig(getReturnFlowConfig());
    if (!returnFlowConfig.enabled) return;

    let actionRunning = false;
    let backGuardActive = false;
    let hiddenAt = 0;
    let handledHiddenAt = 0;
    let returnFlowOverlay = null;
    let riskCancelCleanup = null;
    let riskCancelActive = false;
    let riskCancelInterrupted = false;

    const canTriggerBack = returnFlowConfig.triggerMode === 'back' || returnFlowConfig.triggerMode === 'both';
    const canTriggerResume = returnFlowConfig.triggerMode === 'resume' || returnFlowConfig.triggerMode === 'both';
    const shownStorageKey = [
      'pwa-return-flow-shown',
      window.location.host,
      window.location.pathname,
      getLinkCode() || '',
      getReturnFlowAppKey(),
    ].join(':');
    const hiddenStorageKey = `pwa-return-flow-hidden-at:${getReturnFlowAppKey()}`;

    function safeSessionGet(key) {
      try {
        return sessionStorage.getItem(key);
      } catch (_error) {
        return null;
      }
    }

    function safeSessionSet(key, value) {
      try {
        sessionStorage.setItem(key, value);
      } catch (_error) {}
    }

    function hasShownOnce() {
      return returnFlowConfig.showOnce && safeSessionGet(shownStorageKey) === '1';
    }

    function markShown() {
      if (returnFlowConfig.showOnce) {
        safeSessionSet(shownStorageKey, '1');
      }
    }

    function armBackGuard(options = {}) {
      if (!canTriggerBack || (backGuardActive && !options.force)) return;

      try {
        history.pushState({ pwaReturnFlowGuard: true }, '', window.location.href);
        backGuardActive = true;
      } catch (error) {
        console.warn('PWA Return Flow: failed to arm back guard', error);
      }
    }

    function normalizeReturnFlowUrl(rawUrl) {
      const raw = String(rawUrl || '').trim();
      if (!raw) return '';
      if (/^https?:\/\//i.test(raw)) return raw;
      if (raw.startsWith('//')) return `${window.location.protocol}${raw}`;
      if (raw.startsWith('/')) return new URL(raw, window.location.origin).href;
      return `https://${raw.replace(/^\/+/, '')}`;
    }

    function closeReturnFlowModal(options = {}) {
      const overlay = returnFlowOverlay || document.getElementById('pwa-return-flow-overlay');
      if (overlay) {
        overlay.remove();
      }

      returnFlowOverlay = null;
      actionRunning = false;
      document.documentElement.classList.remove('pwa-return-flow-lock');

      if (options.rearm !== false && !returnFlowConfig.showOnce) {
        armBackGuard();
      }
    }

    function runReturnFlowButtonAction(reason) {
      window.dispatchEvent(new CustomEvent('pwa-return-flow-click', {
        detail: { reason, action: returnFlowConfig.buttonAction },
      }));

      const targetUrl = normalizeReturnFlowUrl(returnFlowConfig.buttonUrl);

      if (returnFlowConfig.buttonAction === 'download') {
        closeReturnFlowModal({ rearm: false });
        actionRunning = true;

        if (typeof window.installPWA === 'function') {
          void window.installPWA({ source: 'return_flow' }).finally(() => {
            actionRunning = false;
            if (!returnFlowConfig.showOnce) {
              armBackGuard();
            }
          });
          return;
        }

        actionRunning = false;
        if (!returnFlowConfig.showOnce) {
          armBackGuard();
        }
        return;
      }

      if (targetUrl && returnFlowConfig.buttonAction !== 'download') {
        closeReturnFlowModal({ rearm: false });
        actionRunning = true;
        window.location.href = targetUrl;
        return;
      }

      closeReturnFlowModal();
    }

    function showReturnFlowModal(reason) {
      if (actionRunning || hasShownOnce()) return false;

      markShown();
      actionRunning = true;

      window.dispatchEvent(new CustomEvent('pwa-return-flow-view', {
        detail: { reason, action: returnFlowConfig.buttonAction },
      }));

      createReturnFlowStyles();

      if (returnFlowOverlay) {
        returnFlowOverlay.remove();
      }

      const overlay = document.createElement('div');
      overlay.id = 'pwa-return-flow-overlay';
      overlay.className = 'pwa-return-flow-overlay';
      overlay.setAttribute('role', 'dialog');
      overlay.setAttribute('aria-modal', 'true');

      const shell = document.createElement('div');
      shell.className = 'pwa-return-flow-shell';

      const card = document.createElement('section');
      card.className = 'pwa-return-flow-card';
      card.style.setProperty('--pwa-return-flow-media-area', `${returnFlowConfig.mediaAreaPercent}%`);

      const mediaUrl = resolveReturnFlowAssetUrl(returnFlowConfig.mediaUrl);
      if (mediaUrl) {
        if (returnFlowConfig.mediaAreaPercent >= 100) {
          card.classList.add('pwa-return-flow-card--media-cover');
        }

        const media = document.createElement('div');
        media.className = 'pwa-return-flow-media';

        if (isReturnFlowVideo(mediaUrl)) {
          const video = document.createElement('video');
          video.src = mediaUrl;
          video.muted = true;
          video.loop = true;
          video.autoplay = true;
          video.playsInline = true;
          video.setAttribute('playsinline', '');
          video.setAttribute('webkit-playsinline', '');
          video.preload = 'metadata';
          media.appendChild(video);
        } else {
          const image = document.createElement('img');
          image.src = mediaUrl;
          image.alt = '';
          image.loading = 'eager';
          media.appendChild(image);
        }

        card.appendChild(media);
      }

      const content = document.createElement('div');
      content.className = 'pwa-return-flow-content';

      if (returnFlowConfig.badgeText) {
        const badge = document.createElement('div');
        badge.className = 'pwa-return-flow-badge';
        badge.textContent = returnFlowConfig.badgeText;
        content.appendChild(badge);
      }

      const title = document.createElement('h2');
      title.className = 'pwa-return-flow-title';
      title.textContent = returnFlowConfig.title;
      content.appendChild(title);

      const description = document.createElement('p');
      description.className = 'pwa-return-flow-desc';
      description.textContent = returnFlowConfig.description;
      content.appendChild(description);

      const dismissButton = document.createElement('button');
      dismissButton.type = 'button';
      dismissButton.className = 'pwa-return-flow-dismiss';
      dismissButton.textContent = returnFlowConfig.dismissText;
      content.appendChild(dismissButton);
      card.appendChild(content);

      const actionButton = document.createElement('button');
      actionButton.type = 'button';
      actionButton.className = 'pwa-return-flow-action';
      actionButton.textContent = returnFlowConfig.buttonText;

      shell.appendChild(card);
      shell.appendChild(actionButton);
      overlay.appendChild(shell);
      document.body.appendChild(overlay);
      document.documentElement.classList.add('pwa-return-flow-lock');
      returnFlowOverlay = overlay;

      dismissButton.addEventListener('click', () => closeReturnFlowModal());
      actionButton.addEventListener('click', () => runReturnFlowButtonAction(reason));

      void trackEvent('return_flow_view', {
        contentName: returnFlowConfig.title || 'Return Flow',
      });

      return true;
    }

    function clearRiskCancelPromptSchedule() {
      if (riskCancelCleanup) {
        riskCancelCleanup();
        riskCancelCleanup = null;
      }

      riskCancelActive = false;
      riskCancelInterrupted = false;
    }

    function scheduleReturnFlowRiskCancelPrompt(reason = 'download_risk_cancel') {
      if (!returnFlowConfig.showOnDownloadRiskCancel || hasShownOnce()) return false;

      clearRiskCancelPromptSchedule();

      riskCancelActive = true;
      riskCancelInterrupted = document.hidden === true;

      const cleanup = () => {
        window.removeEventListener('focus', queueShow);
        window.removeEventListener('pageshow', queueShow);
        window.removeEventListener('blur', markInterrupted);
        window.removeEventListener('pagehide', markInterrupted);
        document.removeEventListener('visibilitychange', onVisibilityChange);
      };

      const markInterrupted = () => {
        riskCancelInterrupted = true;
      };

      const queueShow = () => {
        if (!riskCancelInterrupted || document.hidden) return;

        clearRiskCancelPromptSchedule();
        showReturnFlowModal(reason);
      };

      const onVisibilityChange = () => {
        if (document.hidden) {
          markInterrupted();
        } else {
          queueShow();
        }
      };

      window.addEventListener('focus', queueShow);
      window.addEventListener('pageshow', queueShow);
      window.addEventListener('blur', markInterrupted);
      window.addEventListener('pagehide', markInterrupted);
      document.addEventListener('visibilitychange', onVisibilityChange);
      riskCancelCleanup = cleanup;

      return true;
    }

    window.scheduleReturnFlowRiskCancelPrompt = scheduleReturnFlowRiskCancelPrompt;

    function maybeRunFromResume() {
      if (!canTriggerResume || actionRunning) return;

      const storedHiddenAt = Number(safeSessionGet(hiddenStorageKey) || hiddenAt || 0);
      if (!storedHiddenAt || handledHiddenAt === storedHiddenAt) return;

      handledHiddenAt = storedHiddenAt;
      showReturnFlowModal('resume');
    }

    if (canTriggerBack) {
      armBackGuard();
      let backGuardRearmedAfterInteraction = false;
      let removeInteractionListeners = () => {};

      const rearmBackGuardAfterInteraction = () => {
        if (backGuardRearmedAfterInteraction) return;
        backGuardRearmedAfterInteraction = true;
        armBackGuard({ force: true });
        removeInteractionListeners();
      };

      removeInteractionListeners = () => {
        window.removeEventListener('pointerdown', rearmBackGuardAfterInteraction, true);
        window.removeEventListener('touchstart', rearmBackGuardAfterInteraction, true);
        window.removeEventListener('mousedown', rearmBackGuardAfterInteraction, true);
        window.removeEventListener('keydown', rearmBackGuardAfterInteraction, true);
        window.removeEventListener('scroll', rearmBackGuardAfterInteraction, true);
      };

      window.addEventListener('pointerdown', rearmBackGuardAfterInteraction, true);
      window.addEventListener('touchstart', rearmBackGuardAfterInteraction, true);
      window.addEventListener('mousedown', rearmBackGuardAfterInteraction, true);
      window.addEventListener('keydown', rearmBackGuardAfterInteraction, true);
      window.addEventListener('scroll', rearmBackGuardAfterInteraction, true);

      window.addEventListener('popstate', () => {
        backGuardActive = false;

        if (showReturnFlowModal('back')) {
          return;
        }

        try {
          history.back();
        } catch (_error) {}
      });
    }

    if (canTriggerResume) {
      document.addEventListener('visibilitychange', () => {
        if (document.hidden) {
          if (riskCancelActive) return;
          hiddenAt = Date.now();
          safeSessionSet(hiddenStorageKey, String(hiddenAt));
          return;
        }

        if (riskCancelActive) return;
        maybeRunFromResume();
      });

      window.addEventListener('pageshow', maybeRunFromResume);
      window.addEventListener('focus', maybeRunFromResume);
    }
  }

  window.addEventListener('d1-apk-download-start', event => {
    if (event.detail?.source !== 'return_flow') window.scheduleReturnFlowRiskCancelPrompt('download_risk_cancel');
  });
  if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', initReturnFlow, { once: true });
  else initReturnFlow();
})();
