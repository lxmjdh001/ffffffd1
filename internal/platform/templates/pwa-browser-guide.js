(function(){
  function getBrowserGuideConfig() {
    const config = window.__APP_CONFIG__ || {};
    const guideConfig = config.app?.settings?.browserGuideConfig
      || config.customApp?.otherSettings?.browserGuideConfig
      || {};

    return {
      enabled: guideConfig.enabled === true,
      message: String(guideConfig.message || '').trim().slice(0, 300),
    };
  }

  function isTikTokInAppBrowser() {
    const userAgent = String(navigator.userAgent || '');
    return /tiktok|musical_ly|trill|bytedancewebview|com\.zhiliaoapp\.musically|aweme/i.test(userAgent);
  }

  function createBrowserGuideStyles() {
    return; // Styles are served in the app-scoped stylesheet.
    if (document.getElementById('browser-guide-styles')) return;

    const style = document.createElement('style');
    style.id = 'browser-guide-styles';
    style.textContent = `
      .browser-guide-overlay {
        position: fixed;
        inset: 0;
        z-index: 100002;
        display: flex;
        align-items: center;
        justify-content: center;
        padding: 18px;
        background: rgba(12, 15, 20, 0.72);
        opacity: 0;
        transition: opacity 180ms ease;
      }
      .browser-guide-overlay.show { opacity: 1; }
      .browser-guide-dialog {
        position: relative;
        box-sizing: border-box;
        width: min(430px, 94vw);
        max-height: 92vh;
        overflow: auto;
        padding: 48px 12px 12px;
        border-radius: 26px;
        background: #ffffff;
        box-shadow: 0 28px 80px rgba(0, 0, 0, 0.34);
        transform: translateY(14px) scale(0.98);
        transition: transform 180ms ease;
      }
      .browser-guide-overlay.show .browser-guide-dialog {
        transform: translateY(0) scale(1);
      }
      .browser-guide-close {
        position: absolute;
        top: 8px;
        right: 10px;
        z-index: 3;
        width: 36px;
        height: 36px;
        border: 0;
        border-radius: 50%;
        background: #f0f2f5;
        color: #111318;
        font-size: 26px;
        line-height: 34px;
        cursor: pointer;
      }
      .browser-guide-message {
        position: absolute;
        box-sizing: border-box;
        top: 50%;
        left: 50%;
        z-index: 2;
        width: calc(100% - 32px);
        max-height: calc(100% - 48px);
        overflow-y: auto;
        padding: 14px 16px;
        border: 1px solid rgba(255, 255, 255, 0.28);
        border-radius: 14px;
        background: rgba(12, 15, 20, 0.72);
        box-shadow: 0 10px 30px rgba(0, 0, 0, 0.28);
        color: #ffffff;
        font-size: clamp(17px, 4.6vw, 22px);
        font-weight: 750;
        line-height: 1.45;
        text-align: center;
        text-shadow: 0 1px 2px rgba(0, 0, 0, 0.45);
        white-space: pre-wrap;
        overflow-wrap: anywhere;
        transform: translate(-50%, -50%);
      }
      .browser-guide-visual {
        position: relative;
        overflow: hidden;
        border-radius: 20px;
        background: #d8d8d8;
      }
      .browser-guide-image {
        display: block;
        width: 100%;
        height: auto;
      }
      @media (max-height: 680px) {
        .browser-guide-dialog { padding-top: 42px; }
      }
    `;
    document.head.appendChild(style);
  }

  function isBrowserGuideEnabled() {
    const guideConfig = getBrowserGuideConfig();
    return guideConfig.enabled && Boolean(guideConfig.message) && isTikTokInAppBrowser();
  }

  function showBrowserGuide() {
    const guideConfig = getBrowserGuideConfig();
    if (!isBrowserGuideEnabled() || !document.body) {
      return false;
    }

    if (document.getElementById('browser-guide-overlay')) {
      return true;
    }

    createBrowserGuideStyles();

    const overlay = document.createElement('div');
    overlay.id = 'browser-guide-overlay';
    overlay.className = 'browser-guide-overlay';
    overlay.innerHTML = `
      <div class="browser-guide-dialog" role="dialog" aria-modal="true">
        <button class="browser-guide-close" type="button" aria-label="Close">&times;</button>
        <div class="browser-guide-visual">
          <img class="browser-guide-image" src="/pwa-template-assets/browser-guide.png?v=20260817154000" alt="" aria-hidden="true">
          <div class="browser-guide-message"></div>
        </div>
      </div>
    `;

    overlay.querySelector('.browser-guide-message').textContent = guideConfig.message;
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    document.body.appendChild(overlay);

    const closeGuide = () => {
      overlay.classList.remove('show');
      document.removeEventListener('keydown', onKeydown);
      setTimeout(() => {
        overlay.remove();
        document.body.style.overflow = previousOverflow;
      }, 180);
    };
    const onKeydown = (event) => {
      if (event.key === 'Escape') closeGuide();
    };

    overlay.querySelector('.browser-guide-close').addEventListener('click', closeGuide);
    overlay.addEventListener('click', (event) => {
      if (event.target === overlay) closeGuide();
    });
    document.addEventListener('keydown', onKeydown);
    requestAnimationFrame(() => overlay.classList.add('show'));
    return true;
  }

  window.isBrowserGuideEnabled = isBrowserGuideEnabled;
  window.showBrowserGuide = showBrowserGuide;


})();
