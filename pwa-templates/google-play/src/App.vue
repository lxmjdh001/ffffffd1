<template>
  <div class="google-play-mobile">
    <!-- 顶部导航栏 -->
    <header v-if="appInfo.enableGoogleBadge" class="header" :class="{ 'header-scrolled': isScrolled }">
      <div class="header-left">
        <svg class="play-logo" aria-hidden="true" viewBox="0 0 40 40" xmlns="http://www.w3.org/2000/svg">
          <path fill="none" d="M0,0h40v40H0V0z"></path>
          <g>
            <path d="M19.7,19.2L4.3,35.3c0,0,0,0,0,0c0.5,1.7,2.1,3,4,3c0.8,0,1.5-0.2,2.1-0.6l0,0l17.4-9.9L19.7,19.2z" fill="#EA4335"></path>
            <path d="M35.3,16.4L35.3,16.4l-7.5-4.3l-8.4,7.4l8.5,8.3l7.5-4.2c1.3-0.7,2.2-2.1,2.2-3.6C37.5,18.5,36.6,17.1,35.3,16.4z" fill="#FBBC04"></path>
            <path d="M4.3,4.7C4.2,5,4.2,5.4,4.2,5.8v28.5c0,0.4,0,0.7,0.1,1.1l16-15.7L4.3,4.7z" fill="#4285F4"></path>
            <path d="M19.8,20l8-7.9L10.5,2.3C9.9,1.9,9.1,1.7,8.3,1.7c-1.9,0-3.6,1.3-4,3c0,0,0,0,0,0L19.8,20z" fill="#34A853"></path>
          </g>
        </svg>
        <span class="logo-text">{{ t('header.logo') }}</span>
      </div>
      <div class="header-right">
        <img src="/pwa-template-assets/GooglePlaybestapp-right.png" alt="Best App" class="header-right-image">
      </div>
    </header>

    <!-- 主内容区 -->
    <main class="main-content" :style="{ paddingTop: appInfo.enableGoogleBadge ? '57px' : '0' }">
      <!-- 应用头部信息 -->
      <section class="app-header-section">
        <!-- 骨架屏 -->
        <div v-if="isLoading" class="app-main-info">
          <div class="skeleton app-icon-skeleton"></div>
          <div class="app-text-info">
            <div class="skeleton skeleton-title"></div>
            <div class="skeleton skeleton-text" style="width: 120px;"></div>
            <div class="skeleton skeleton-text" style="width: 100px;"></div>
          </div>
        </div>

        <!-- 实际内容 -->
        <div v-else class="app-main-info">
          <img :src="appInfo.icon" :alt="appInfo.name" class="app-icon-large">
          <div class="app-text-info">
            <h1 class="app-title">
              {{ appInfo.name }}
              <svg v-if="appInfo.enableOfficialVerify" class="verified-icon" width="20" height="20" viewBox="0 0 26 26" fill="none" xmlns="http://www.w3.org/2000/svg">
                <path d="M11.088 2.522a2.4 2.4 0 0 1 3.824 0l.722.952a2.4 2.4 0 0 0 2.24.927l1.183-.162a2.4 2.4 0 0 1 2.704 2.704L21.6 8.127a2.4 2.4 0 0 0 .927 2.239l.952.722a2.4 2.4 0 0 1 0 3.824l-.952.722a2.4 2.4 0 0 0-.927 2.24l.162 1.183a2.4 2.4 0 0 1-2.704 2.704l-1.184-.162a2.4 2.4 0 0 0-2.239.927l-.722.952a2.4 2.4 0 0 1-3.824 0l-.722-.952a2.4 2.4 0 0 0-2.24-.927l-1.183.162a2.4 2.4 0 0 1-2.704-2.704l.162-1.184a2.4 2.4 0 0 0-.927-2.239l-.952-.722a2.4 2.4 0 0 1 0-3.824l.952-.722a2.4 2.4 0 0 0 .927-2.24L4.24 6.944A2.4 2.4 0 0 1 6.943 4.24l1.184.162a2.4 2.4 0 0 0 2.239-.927l.722-.952Z" fill="#4CC0FF"></path>
                <path d="M8.315 11.914a1 1 0 0 1 1.414 0l2.727 2.727 4.2-4.199a1 1 0 0 1 1.414 1.414l-4.907 4.907a1 1 0 0 1-1.414 0l-.014-.015-3.42-3.42a1 1 0 0 1 0-1.414Z" fill="#fff"></path>
              </svg>
            </h1>
            <div class="app-developer-link">{{ appInfo.developer }}</div>
            <div class="app-badges">
              <span class="badge verified-badge">
                <img src="/pwa-template-assets/shield.png" alt="shield" class="shield-icon">
                {{ t('app.verifiedByPlayProtect') }}
              </span>
            </div>
          </div>
        </div>

        <!-- 统计信息 -->
        <div v-if="isLoading" class="app-stats-row">
          <div class="stat-col">
            <div class="skeleton skeleton-stat"></div>
            <div class="skeleton skeleton-text-small"></div>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-col">
            <div class="skeleton skeleton-stat"></div>
            <div class="skeleton skeleton-text-small"></div>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-col">
            <div class="skeleton skeleton-stat"></div>
            <div class="skeleton skeleton-text-small"></div>
          </div>
        </div>
        <div v-else class="app-stats-row">
          <div class="stat-col">
            <div class="stat-main">4.9<span class="star">★</span></div>
            <div class="stat-sub">{{ appInfo.reviewCount }} {{ t('app.rating') }}</div>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-col">
            <div class="stat-main">{{ appInfo.downloads }}</div>
            <div class="stat-sub">{{ t('app.downloads') }}</div>
          </div>
          <div class="stat-divider"></div>
          <div class="stat-col">
            <img v-if="!appInfo.ageRating" src="/pwa-template-assets/age/age0.png" alt="No rating" class="age-rating-icon">
            <img v-else-if="appInfo.ageRating === 3" src="/pwa-template-assets/age/age3.webp" alt="3+" class="age-rating-icon">
            <img v-else-if="appInfo.ageRating === 7" src="/pwa-template-assets/age/age7.png" alt="7+" class="age-rating-icon">
            <img v-else-if="appInfo.ageRating === 12" src="/pwa-template-assets/age/age12.png" alt="12+" class="age-rating-icon">
            <img v-else-if="appInfo.ageRating === 18" src="/pwa-template-assets/age/age18.png" alt="18+" class="age-rating-icon">
            <div class="stat-sub">{{ t('app.contentRating') }}</div>
          </div>
        </div>

        <!-- 扫码演示 -->
        <div v-if="!isLoading && shouldShowScanGuide" class="scan-demo-card">
          <div class="scan-demo-header">
            <div class="scan-demo-title">{{ scanCopy.title }}</div>
            <div class="scan-demo-desc">{{ scanCopy.subtitle }}</div>
          </div>
          <div class="scan-demo-body">
            <div class="scan-demo-col">
              <div class="scan-phone-wrapper">
                <img src="/pwa-template-assets/scan/scan-phone.png" alt="Scanning illustration" class="scan-demo-image">
                <div class="scan-phone-scan-line"></div>
              </div>
              <div class="scan-demo-label">{{ scanCopy.scanning }}</div>
            </div>
            <div class="scan-demo-col">
              <div class="qr-wrapper" aria-live="polite">
                <img v-if="qrCodeUrl" :src="qrCodeUrl" alt="QR code for this page" class="qr-image">
                <div v-else class="qr-placeholder skeleton"></div>
                <span class="qr-corner corner-tl"></span>
                <span class="qr-corner corner-tr"></span>
                <span class="qr-corner corner-bl"></span>
                <span class="qr-corner corner-br"></span>
              </div>
              <div class="scan-demo-label">{{ scanCopy.qrcode }}</div>
            </div>
            <div class="scan-demo-col">
              <div class="scan-result-frame">
                <img src="/pwa-template-assets/scan/scan-result.png" alt="Scan result" class="scan-result-image">
                <div class="scan-result-content">
                  <div v-if="appInfo.icon" class="scan-result-icon-wrapper">
                    <img :src="appInfo.icon" :alt="appInfo.name" class="scan-result-icon">
                  </div>
                  <div v-else class="scan-result-icon-placeholder">{{ scanResultInitial }}</div>
                  <div class="scan-result-name">{{ appInfo.name || scanCopy.fallbackName }}</div>
                  <button class="scan-mini-install-btn">
                    {{ t('app.install') }}
                  </button>
                </div>
              </div>
              <div class="scan-demo-label">{{ scanCopy.result }}</div>
            </div>
          </div>
        </div>

        <!-- 安装按钮 -->
        <button class="install-button" :disabled="isLoading" @click="handleInstall">
          {{ isLoading ? '' : t('app.install') }}
          <div v-if="isLoading" class="skeleton skeleton-button-text"></div>
        </button>

        <!-- 分享和收藏 -->
        <div class="action-row">
          <button class="action-btn" :disabled="isLoading">
            <svg v-if="!isLoading" width="20" height="20" viewBox="0 0 24 24">
              <path fill="currentColor" d="M18 16.08c-.76 0-1.44.3-1.96.77L8.91 12.7c.05-.23.09-.46.09-.7s-.04-.47-.09-.7l7.05-4.11c.54.5 1.25.81 2.04.81 1.66 0 3-1.34 3-3s-1.34-3-3-3-3 1.34-3 3c0 .24.04.47.09.7L8.04 9.81C7.5 9.31 6.79 9 6 9c-1.66 0-3 1.34-3 3s1.34 3 3 3c.79 0 1.5-.31 2.04-.81l7.12 4.16c-.05.21-.08.43-.08.65 0 1.61 1.31 2.92 2.92 2.92 1.61 0 2.92-1.31 2.92-2.92s-1.31-2.92-2.92-2.92z"/>
            </svg>
            <div v-if="isLoading" class="skeleton skeleton-icon"></div>
            <span v-if="!isLoading">{{ t('app.share') }}</span>
          </button>
          <button class="action-btn" :disabled="isLoading">
            <svg v-if="!isLoading" width="20" height="20" viewBox="0 0 24 24">
              <path fill="currentColor" d="M17 3H7c-1.1 0-2 .9-2 2v16l7-3 7 3V5c0-1.1-.9-2-2-2zm0 15l-5-2.18L7 18V5h10v13z"/>
            </svg>
            <div v-if="isLoading" class="skeleton skeleton-icon"></div>
            <span v-if="!isLoading">{{ t('app.addToWishlist') }}</span>
          </button>
        </div>
      </section>

      <!-- 功能截图 -->
      <section class="screenshots-section">
        <div v-if="isLoading" class="screenshots-scroll">
          <div v-for="i in 3" :key="i" class="screenshot-card">
            <div class="skeleton screenshot-skeleton"></div>
          </div>
        </div>
        <div v-else class="screenshots-scroll" ref="screenshotsRef">
          <div
            v-for="(screenshot, index) in appInfo.screenshots"
            :key="index"
            class="screenshot-card"
          >
            <video
              v-if="isVideoAsset(screenshot.image)"
              :src="screenshot.image"
              :title="screenshot.title"
              controls
              muted
              playsinline
              preload="metadata"
            ></video>
            <img v-else :src="screenshot.image" :alt="screenshot.title">
          </div>
        </div>
      </section>

      <!-- 关于此应用 -->
      <section class="about-section">
        <div class="section-header">
          <h2>{{ t('app.aboutApp') }}</h2>
          <button class="arrow-btn">
            <svg width="24" height="24" viewBox="0 0 24 24">
              <path fill="currentColor" d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/>
            </svg>
          </button>
        </div>
        <p class="app-description" :class="{ expanded: descExpanded }">
          {{ appInfo.description }}
        </p>
        <button class="expand-btn" @click="descExpanded = !descExpanded">
          <span>{{ descExpanded ? t('app.collapse') : t('app.viewMore') }}</span>
          <svg width="18" height="18" viewBox="0 0 24 24" :class="{ rotated: descExpanded }">
            <path fill="currentColor" d="M7.41 8.59L12 13.17l4.59-4.58L18 10l-6 6-6-6 1.41-1.41z"/>
          </svg>
        </button>

        <div class="update-date-row">
          <div class="update-label">{{ t('app.updatedOn') }}</div>
          <div class="update-value">{{ appInfo.updateDate }}</div>
        </div>

        <div v-if="appInfo.tags && appInfo.tags.length > 0" class="category-tags">
          <span
            v-for="(tag, index) in appInfo.tags"
            :key="index"
            class="category-tag"
            :class="{ primary: index === 0 }"
          >
            {{ tag }}
          </span>
        </div>
      </section>

      <transition name="stay-prompt-fade">
        <div
          v-if="stayPromptVisible"
          class="stay-prompt-overlay"
          @click.self="dismissStayPrompt"
        >
          <div class="stay-prompt-card" role="dialog" aria-modal="true" :aria-label="appInfo.name || t('app.install')">
            <button class="stay-prompt-close" type="button" @click="dismissStayPrompt" aria-label="Close">
              <svg width="18" height="18" viewBox="0 0 24 24" aria-hidden="true">
                <path fill="currentColor" d="M18.3 5.71 12 12l6.3 6.29-1.41 1.41L10.59 13.41 4.29 19.7 2.88 18.29 9.17 12 2.88 5.71 4.29 4.29l6.3 6.3 6.29-6.3z"/>
              </svg>
            </button>
            <div class="stay-prompt-app">
              <img :src="appInfo.icon" :alt="appInfo.name" class="stay-prompt-app-icon">
              <div class="stay-prompt-app-info">
                <div class="stay-prompt-app-name">{{ appInfo.name }}</div>
                <div class="stay-prompt-app-developer">{{ appInfo.developer }}</div>
                <span class="stay-prompt-badge">{{ t('app.verifiedByPlayProtect') }}</span>
              </div>
            </div>
            <p class="stay-prompt-message">{{ appInfo.stayPromptText }}</p>
            <button class="stay-prompt-install" type="button" @click="handleStayPromptInstall">
              {{ t('app.install') }}
            </button>
          </div>
        </div>
      </transition>

      <!-- 数据安全 -->
      <section class="data-safety-section">
        <div class="section-header">
          <h2>{{ t('app.dataSafety') }}</h2>
          <button class="arrow-btn">
            <svg width="24" height="24" viewBox="0 0 24 24">
              <path fill="currentColor" d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/>
            </svg>
          </button>
        </div>
        <p class="safety-description">
          {{ t('app.dataSafetyDesc') }}
        </p>

        <div class="safety-info-card">
          <div class="safety-item">
            <svg width="24" height="24" viewBox="0 0 24 24" class="safety-icon">
              <path fill="currentColor" d="M18 16.08c-.76 0-1.44.3-1.96.77L8.91 12.7c.05-.23.09-.46.09-.7s-.04-.47-.09-.7l7.05-4.11c.54.5 1.25.81 2.04.81 1.66 0 3-1.34 3-3s-1.34-3-3-3-3 1.34-3 3c0 .24.04.47.09.7L8.04 9.81C7.5 9.31 6.79 9 6 9c-1.66 0-3 1.34-3 3s1.34 3 3 3c.79 0 1.5-.31 2.04-.81l7.12 4.16c-.05.21-.08.43-.08.65 0 1.61 1.31 2.92 2.92 2.92 1.61 0 2.92-1.31 2.92-2.92s-1.31-2.92-2.92-2.92z"/>
            </svg>
            <div class="safety-text">
              <div class="safety-title">{{ t('app.mayShareData') }}</div>
              <div class="safety-subtitle">{{ t('app.deviceIds') }}</div>
            </div>
          </div>

          <div class="safety-item">
            <svg width="24" height="24" viewBox="0 0 24 24" class="safety-icon">
              <path fill="currentColor" d="M19.35 10.04C18.67 6.59 15.64 4 12 4 9.11 4 6.6 5.64 5.35 8.04 2.34 8.36 0 10.91 0 14c0 3.31 2.69 6 6 6h13c2.76 0 5-2.24 5-5 0-2.64-2.05-4.78-4.65-4.96z"/>
            </svg>
            <div class="safety-text">
              <div class="safety-title">{{ t('app.mayCollectData') }}</div>
              <div class="safety-subtitle">{{ t('app.locationPersonal') }}</div>
            </div>
          </div>

          <div class="safety-item">
            <svg width="24" height="24" viewBox="0 0 24 24" class="safety-icon">
              <path fill="currentColor" d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"/>
            </svg>
            <div class="safety-text">
              <div class="safety-title">{{ t('app.dataEncrypted') }}</div>
            </div>
          </div>

          <div class="safety-item">
            <svg width="24" height="24" viewBox="0 0 24 24" class="safety-icon">
              <path fill="currentColor" d="M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z"/>
            </svg>
            <div class="safety-text">
              <div class="safety-title">{{ t('app.dataCanDelete') }}</div>
            </div>
          </div>

          <button class="see-details-btn">{{ t('app.seeDetails') }}</button>
        </div>
      </section>

      <!-- 评分和评价 -->
      <section class="ratings-section">
        <div class="section-header">
          <h2>{{ t('app.ratingAndReviews') }}</h2>
          <button class="arrow-btn">
            <svg width="24" height="24" viewBox="0 0 24 24">
              <path fill="currentColor" d="M8.59 16.59L13.17 12 8.59 7.41 10 6l6 6-6 6-1.41-1.41z"/>
            </svg>
          </button>
        </div>

        <div class="ratings-verified">
          <span>{{ t('app.ratingsVerified') }}</span>
          <svg width="16" height="16" viewBox="0 0 24 24">
            <path fill="currentColor" d="M11 18h2v-2h-2v2zm1-16C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 18c-4.41 0-8-3.59-8-8s3.59-8 8-8 8 3.59 8 8-3.59 8-8 8zm0-14c-2.21 0-4 1.79-4 4h2c0-1.1.9-2 2-2s2 .9 2 2c0 2-3 1.75-3 5h2c0-2.25 3-2.5 3-5 0-2.21-1.79-4-4-4z"/>
          </svg>
        </div>

        <div class="device-filter-tabs">
          <button class="device-tab active">
            <svg width="18" height="18" viewBox="0 0 24 24">
              <path fill="currentColor" d="M17 1.01L7 1c-1.1 0-2 .9-2 2v18c0 1.1.9 2 2 2h10c1.1 0 2-.9 2-2V3c0-1.1-.9-1.99-2-1.99zM17 19H7V5h10v14z"/>
            </svg>
            <span>{{ t('app.phone') }}</span>
          </button>
          <button class="device-tab">
            <svg width="18" height="18" viewBox="0 0 24 24">
              <path fill="currentColor" d="M20 18c1.1 0 1.99-.9 1.99-2L22 6c0-1.1-.9-2-2-2H4c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2H0v2h24v-2h-4zM4 6h16v10H4V6z"/>
            </svg>
            <span>{{ t('app.chromebook') }}</span>
          </button>
          <button class="device-tab">
            <svg width="18" height="18" viewBox="0 0 24 24">
              <path fill="currentColor" d="M21 4H3c-1.1 0-2 .9-2 2v12c0 1.1.9 2 2 2h18c1.1 0 2-.9 2-2V6c0-1.1-.9-2-2-2zm0 14H3V6h18v12z"/>
            </svg>
            <span>{{ t('app.tablet') }}</span>
          </button>
        </div>

        <div class="rating-overview">
          <div class="rating-score-box">
            <div class="large-rating">4.9</div>
            <div class="rating-stars">
              <svg v-for="i in 4" :key="i" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">
                <path d="M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z"></path>
              </svg>
              <svg class="xdPrrd" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="url(#c456)">
                <defs>
                  <linearGradient id="c456" x1="0%" y1="0%" x2="100%" y2="0%">
                    <stop stop-opacity="1" offset="50%" class="Z1Dz7b"></stop>
                    <stop stop-opacity="1" offset="50%" class="Wi7KIe"></stop>
                  </linearGradient>
                </defs>
                <path d="M12 17.27L18.18 21l-1.64-7.03L22 9.24l-7.19-.61L12 2 9.19 8.63 2 9.24l5.46 4.73L5.82 21z"></path>
              </svg>
            </div>
            <div class="rating-count-text">{{ appInfo.reviewCount }} {{ t('app.rating') }}</div>
          </div>
          <div class="rating-bars">
            <div v-for="i in 5" :key="i" class="rating-bar-row">
              <span class="bar-label">{{ 6 - i }}</span>
              <div class="bar-bg">
                <div class="bar-fill" :style="{ width: getRatingBarWidth(6 - i) }"></div>
              </div>
            </div>
          </div>
        </div>

        <!-- 用户评论 -->
        <div class="reviews-list">
          <div v-for="(review, index) in reviews" :key="index" class="review-item">
            <div class="review-header">
              <img :src="review.avatar" :alt="review.author" class="reviewer-avatar">
              <div class="reviewer-info">
                <div class="reviewer-name">{{ review.author }}</div>
                <button class="review-menu-btn">
                  <svg width="20" height="20" viewBox="0 0 24 24">
                    <path fill="currentColor" d="M12 8c1.1 0 2-.9 2-2s-.9-2-2-2-2 .9-2 2 .9 2 2 2zm0 2c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2zm0 6c-1.1 0-2 .9-2 2s.9 2 2 2 2-.9 2-2-.9-2-2-2z"/>
                  </svg>
                </button>
              </div>
            </div>
            <div class="review-stars">
              <span v-for="j in review.rating" :key="j" class="star-filled">★</span>
            </div>
            <div class="review-date">{{ review.date }}</div>
            <p class="review-text">{{ review.text }}</p>
            <div class="review-helpful-count">
              {{ review.helpful }} {{ t('app.peopleFoundHelpful') }}
            </div>
            <div class="review-actions">
              <span class="helpful-question">{{ t('app.didYouFindHelpful') }}</span>
              <div class="helpful-buttons">
                <button class="helpful-btn">{{ t('app.yes') }}</button>
                <button class="helpful-btn">{{ t('app.no') }}</button>
              </div>
            </div>
          </div>
        </div>
      </section>

      <!-- Footer Links -->
      <section class="footer-section">
        <div class="footer-group">
          <h3 class="footer-heading">{{ t('footer.googlePlay') }}</h3>
          <a href="#" class="footer-link">{{ t('footer.playPass') }}</a>
          <a href="#" class="footer-link">{{ t('footer.playPoints') }}</a>
          <a href="#" class="footer-link">{{ t('footer.giftCards') }}</a>
          <a href="#" class="footer-link">{{ t('footer.redeem') }}</a>
          <a href="#" class="footer-link">{{ t('footer.refundPolicy') }}</a>
        </div>

        <div class="footer-group">
          <h3 class="footer-heading">{{ t('footer.kidsFamily') }}</h3>
          <a href="#" class="footer-link">{{ t('footer.parentGuide') }}</a>
          <a href="#" class="footer-link">{{ t('footer.familySharing') }}</a>
        </div>

        <div class="footer-bottom-links">
          <a href="#" class="footer-bottom-link">{{ t('footer.termsOfService') }}</a>
          <a href="#" class="footer-bottom-link">{{ t('footer.privacy') }}</a>
          <a href="#" class="footer-bottom-link">{{ t('footer.aboutGooglePlay') }}</a>
        </div>

        <div class="footer-bottom-links">
          <a href="#" class="footer-bottom-link">{{ t('footer.developers') }}</a>
          <a href="#" class="footer-bottom-link">{{ t('footer.googleStore') }}</a>
        </div>
      </section>
    </main>

    <!-- 底部导航栏 -->
    <nav class="bottom-nav">
      <button class="nav-item">
        <svg width="24" height="24" viewBox="0 0 21 16" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path fill-rule="evenodd" clip-rule="evenodd" d="M19.3 8.4C19.2169 7.65213 19.0648 6.90427 18.9009 6.09902C18.8676 5.93522 18.8338 5.76904 18.8 5.6L18.8 5.59986C18.7 5.1 18.7 5.09996 18.6 4.7L18.5 4.4C18.2 1.8 16 0 13.3 0H6.7C4.1 0 1.8 1.8 1.4 4.4C1.4 4.43174 1.4 4.4534 1.3968 4.47458C1.38993 4.52014 1.36826 4.56347 1.3 4.7C1.3 5.1 1.3 5.1 1.2 5.6C1.1 6.05 1.025 6.525 0.95 7C0.875 7.475 0.8 7.95 0.7 8.4C0.1 11.9 0 12.5 0 12.7C0 14.2 1.2 15.5 2.8 15.5C3.6 15.5 4.3 15.2 4.8 14.7L7.7 11.9H12.4L15.3 14.8C15.8 15.3 16.5 15.6 17.3 15.6C18.8 15.6 20.1 14.4 20.1 12.8C20.0055 12.5165 19.911 11.9651 19.3946 8.95177L19.3 8.4ZM13 5C13.4971 5 13.9 4.59706 13.9 4.1C13.9 3.60294 13.4971 3.2 13 3.2C12.5029 3.2 12.1 3.60294 12.1 4.1C12.1 4.59706 12.5029 5 13 5ZM15.8 6C15.8 6.49706 15.3971 6.9 14.9 6.9C14.4029 6.9 14 6.49706 14 6C14 5.50294 14.4029 5.1 14.9 5.1C15.3971 5.1 15.8 5.50294 15.8 6ZM10.5 5.4C10.2 5.7 10.2 6.3 10.5 6.6C10.8 6.9 11.4 6.9 11.7 6.6C12 6.3 12 5.7 11.7 5.4C11.4 5.1 10.9 5.1 10.5 5.4ZM13 8.8C13.4971 8.8 13.9 8.39706 13.9 7.9C13.9 7.40294 13.4971 7 13 7C12.5029 7 12.1 7.40294 12.1 7.9C12.1 8.39706 12.5029 8.8 13 8.8ZM6.4 3.5H7.6V5.4H9.5V6.6H7.6V8.5H6.4V6.6H4.5V5.4H6.4V3.5ZM16.5 13.3C16.7 13.5 16.9 13.6 17.2 13.6C17.8 13.6 18.2 13.2 18.2 12.6C18.2 12.7 16.8 4.8 16.8 4.7C16.5 3 15 1.8 13.3 1.8H6.7C4.9 1.8 3.5 3 3.2 4.7C3.2 4.8 1.8 12.7 1.8 12.7C1.8 13.3 2.3 13.7 2.8 13.7C3.1 13.7 3.3 13.6 3.5 13.4L6.9 10H13.1L13.4 10.2L16.5 13.3Z" fill="currentColor"/>
        </svg>
        <span>{{ t('navigation.games') }}</span>
      </button>
      <button class="nav-item active">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path fill-rule="evenodd" clip-rule="evenodd" d="M15 4H18C19.1 4 20 4.9 20 6V9C20 10.1 19.1 11 18 11H15C13.9 11 13 10.1 13 9V6C13 4.9 13.9 4 15 4ZM9 13H6C4.9 13 4 13.9 4 15V18C4 19.1 4.9 20 6 20H9C10.1 20 11 19.1 11 18V15C11 13.9 10.1 13 9 13ZM18 13H15C13.9 13 13 13.9 13 15V18C13 19.1 13.9 20 15 20H18C19.1 20 20 19.1 20 18V15C20 13.9 19.1 13 18 13ZM9 4H6C4.9 4 4 4.9 4 6V9C4 10.1 4.9 11 6 11H9C10.1 11 11 10.1 11 9V6C11 4.9 10.1 4 9 4Z" fill="currentColor"/>
        </svg>
        <span>{{ t('navigation.apps') }}</span>
      </button>
      <button class="nav-item">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path fill-rule="evenodd" clip-rule="evenodd" d="M3 2V22H21V2H3ZM5 20H19V4H5V20ZM9 7H6V5H9V7ZM18 7H15V5H18V7ZM6 19H9V17H6V19ZM18 19H15V17H18V19ZM15 15H18V13H15V15ZM9 15H6V13H9V15ZM15 11H18V9H15V11ZM9 11H6V9H9V11Z" fill="currentColor"/>
        </svg>
        <span>{{ t('navigation.movies') }}</span>
      </button>
      <button class="nav-item">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path fill-rule="evenodd" clip-rule="evenodd" d="M12.4996 6.36584L14.001 7.65237V4H11.001V7.65075L12.4996 6.36584ZM10 2H11.001H14.001H15H16.998C18.6461 2 20.001 3.35397 20.001 5.002V18.998C20.001 20.646 18.6461 22 16.998 22H4V2H10ZM18.001 5.002C18.001 4.459 17.542 4 16.998 4H16.001V12L12.5 9L9.001 12V4H6V20H16.998C17.542 20 18.001 19.541 18.001 18.998V5.002Z" fill="currentColor"/>
        </svg>
        <span>{{ t('navigation.books') }}</span>
      </button>
      <button class="nav-item">
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
          <path fill-rule="evenodd" clip-rule="evenodd" d="M11.9995 20.439C13.1543 20.787 17.2264 22 17.6293 22C18.4311 22 18.928 21.578 19.154 21.325C19.7049 20.7081 19.7029 20.0604 19.6999 19.0794L19.6999 19.074C19.6989 18.647 19.6299 16.111 19.6009 15.125C20.2258 14.252 21.8914 11.907 22.1604 11.5C22.7292 10.643 23.2201 9.901 22.8972 8.908C22.5724 7.90856 21.7594 7.61034 20.8112 7.26259L20.8096 7.262C20.3747 7.103 17.7853 6.254 16.8195 5.942C16.2026 5.107 14.518 2.848 14.221 2.476L14.2198 2.47445C13.5875 1.68311 13.0416 1 11.9995 1C10.9577 1 10.4108 1.684 9.77797 2.477C9.48103 2.848 7.79639 5.107 7.18052 5.942C6.21372 6.255 3.62427 7.103 3.18436 7.265C2.24156 7.61 1.42773 7.908 1.1028 8.908C0.779871 9.901 1.27077 10.643 1.83965 11.501C2.10059 11.894 3.77424 14.252 4.39911 15.125C4.37011 16.111 4.30113 18.646 4.29913 19.074V19.0741C4.29613 20.058 4.29415 20.708 4.84501 21.324C5.06996 21.576 5.56686 22 6.37069 22C6.7726 22 10.8447 20.787 11.9995 20.439ZM17.6018 15.1838C17.6437 16.6103 17.6991 18.7493 17.6999 19.0787C17.7021 19.8051 17.6963 19.9322 17.6736 19.9767C17.5616 19.9504 17.418 19.9144 17.2472 19.8699C16.8391 19.7634 16.2949 19.6126 15.6462 19.4271C14.6587 19.1447 13.4965 18.8013 12.5766 18.5241L11.9995 18.3502L11.4224 18.5241C10.5029 18.8012 9.34041 19.1447 8.35292 19.4271C7.7042 19.6126 7.16005 19.7634 6.75206 19.8699C6.58148 19.9145 6.43802 19.9504 6.32604 19.9766C6.30304 19.9326 6.2969 19.8071 6.29912 19.0801C6.30067 18.7488 6.35718 16.5803 6.39824 15.1838L6.41807 14.5095L6.02543 13.9609C5.19866 12.8058 3.70925 10.7011 3.50581 10.3947C3.01485 9.65422 2.98744 9.57977 3.00475 9.52653C3.02422 9.46662 3.06796 9.4373 3.87165 9.1432C4.20463 9.02058 6.39401 8.29883 7.79654 7.84477L8.40835 7.64669L8.79007 7.12916C9.57143 6.06978 11.1071 4.01707 11.3394 3.72674C11.8852 3.04281 11.9401 3 11.9995 3C12.049 3 12.0824 3.02198 12.403 3.40831C12.4693 3.48831 12.5251 3.55748 12.6586 3.72451C12.8889 4.01303 14.4014 6.03473 15.2108 7.1304L15.5929 7.64752L16.2047 7.84516C17.4867 8.25931 19.7877 9.01784 20.1229 9.1404L20.1237 9.1407C20.2142 9.17389 20.2145 9.17398 20.3015 9.20614C20.9377 9.44213 20.977 9.47051 20.9951 9.52605C21.0125 9.57968 20.9851 9.65415 20.4941 10.3939C20.2859 10.7088 18.8457 12.7438 17.9746 13.9609L17.5819 14.5095L17.6018 15.1838Z" fill="currentColor"/>
        </svg>
        <span>{{ t('navigation.kids') }}</span>
      </button>
    </nav>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, watch, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { languageMap } from './i18n';
import QRCode from 'qrcode';
import { trackVisitEvent, requestNotificationAndSubscribe } from './utils/push';

const { t, locale } = useI18n();
const descExpanded = ref(false);
const screenshotsRef = ref<HTMLElement | null>(null);
const isScrolled = ref(false);
const languageMenuOpen = ref(false);
const isLoading = ref(true);
const shouldShowScanGuide = ref(false);
const enableScanGuide = ref(false);
const qrCodeUrl = ref('');
const stayPromptVisible = ref(false);
const stayPromptDismissed = ref(false);
const hasInstallIntent = ref(false);
const STAY_PROMPT_DELAY_MS = 30000;
let stayPromptTimer: number | null = null;

// 检测是否为预览模式（通过 URL 参数判断）
const isPreviewMode = () => {
  if (typeof window === 'undefined') return false;
  const urlParams = new URLSearchParams(window.location.search);
  return urlParams.get('preview') === 'true';
};

const scanCopy = computed(() => ({
  title: t('scan.title'),
  subtitle: t('scan.subtitle'),
  scanning: t('scan.scanning'),
  qrcode: t('scan.qrcode'),
  result: t('scan.result'),
  fallbackName: t('scan.fallbackName')
}));

const changeLanguage = (lang: string) => {
  locale.value = lang;
  languageMenuOpen.value = false;
};

const handleScroll = () => {
  isScrolled.value = window.scrollY > 0;
};

const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement;
  if (!target.closest('.language-selector-wrapper')) {
    languageMenuOpen.value = false;
  }
};

const clearStayPromptTimer = () => {
  if (typeof window === 'undefined' || stayPromptTimer === null) return;
  window.clearTimeout(stayPromptTimer);
  stayPromptTimer = null;
};

const scheduleStayPrompt = () => {
  if (typeof window === 'undefined') return;

  clearStayPromptTimer();
  stayPromptVisible.value = false;

  if (
    isLoading.value ||
    hasInstallIntent.value ||
    stayPromptDismissed.value ||
    !appInfo.value.stayPromptText.trim()
  ) {
    return;
  }

  stayPromptTimer = window.setTimeout(() => {
    if (!hasInstallIntent.value && !stayPromptDismissed.value && appInfo.value.stayPromptText.trim()) {
      stayPromptVisible.value = true;
    }
  }, STAY_PROMPT_DELAY_MS);
};

const resetStayPromptState = () => {
  hasInstallIntent.value = false;
  stayPromptDismissed.value = false;
  scheduleStayPrompt();
};

const markInstallIntent = () => {
  hasInstallIntent.value = true;
  stayPromptVisible.value = false;
  clearStayPromptTimer();
};

const dismissStayPrompt = () => {
  stayPromptDismissed.value = true;
  stayPromptVisible.value = false;
  clearStayPromptTimer();
};

const updateScanGuideVisibility = () => {
  if (typeof window === 'undefined') return;
  const isDesktop = window.innerWidth > 768;
  shouldShowScanGuide.value = enableScanGuide.value && isDesktop;
};

const updateQRCode = async () => {
  if (typeof window === 'undefined') return;
  try {
    const currentUrl = window.location.href;
    qrCodeUrl.value = await QRCode.toDataURL(currentUrl, {
      width: 600,
      margin: 1,
      color: {
        dark: '#202124',
        light: '#00000000'
      }
    });
  } catch (error) {
    console.error('Failed to generate QR code:', error);
    qrCodeUrl.value = '';
  }
};

const updatePageTitle = () => {
  document.title = `${appInfo.value.name} - ${t('pageTitle')}`;
};

watch(locale, () => {
  updatePageTitle();
});

// 更新应用信息的通用方法
const updateAppInfo = (data: any) => {
  if (data.name) appInfo.value.name = data.name;
  if (data.developer) appInfo.value.developer = data.developer;
  if (data.icon) appInfo.value.icon = data.icon;
  if (data.rating) appInfo.value.rating = data.rating;
  if (data.reviewCount) appInfo.value.reviewCount = data.reviewCount;
  if (data.downloads) appInfo.value.downloads = data.downloads;
  if (data.ageRating !== undefined) appInfo.value.ageRating = data.ageRating;
  if (data.description) appInfo.value.description = data.description;
  if (data.updateDate) appInfo.value.updateDate = data.updateDate;
  if (data.screenshots && Array.isArray(data.screenshots)) {
    appInfo.value.screenshots = data.screenshots;
  }
  if (data.reviews && Array.isArray(data.reviews)) {
    reviews.value = data.reviews;
  }
  if (data.tags && Array.isArray(data.tags)) {
    appInfo.value.tags = data.tags;
  }
  if (data.stayPromptText !== undefined) {
    appInfo.value.stayPromptText = data.stayPromptText || '';
  }
  if (data.locale && typeof data.locale === 'string') {
    locale.value = data.locale; // 切换预览页面的界面语言
  }
  if (data.enableGoogleBadge !== undefined) appInfo.value.enableGoogleBadge = data.enableGoogleBadge;
  if (data.enableOfficialVerify !== undefined) appInfo.value.enableOfficialVerify = data.enableOfficialVerify;
  if (data.enableQrcode !== undefined) {
    enableScanGuide.value = !!data.enableQrcode;
    updateScanGuideVisibility();
  }
  // 其他设置字段
  if (data.productH5Url) appInfo.value.productH5Url = data.productH5Url;
  if (data.iosRedirectUrl) appInfo.value.iosRedirectUrl = data.iosRedirectUrl;
  if (data.enableApkUpload !== undefined) appInfo.value.enableApkUpload = data.enableApkUpload;
  if (data.apkUrl) appInfo.value.apkUrl = data.apkUrl;
  if (data.packagePriority) appInfo.value.packagePriority = data.packagePriority;
  if (data.iosCompatible !== undefined) appInfo.value.iosCompatible = data.iosCompatible;
  if (data.w2aAutoDownload !== undefined) appInfo.value.w2aAutoDownload = data.w2aAutoDownload;

  isLoading.value = false;
  resetStayPromptState();
};

const getRuntimeApiBase = (apiBaseUrl?: string): string => {
  const configured = apiBaseUrl || import.meta.env.VITE_API_URL || import.meta.env.VITE_API_BASE_URL || 'http://localhost:4000';
  return configured
    .trim()
    .replace(/\/+$/, '')
    .replace(/\/api$/i, '');
};

const getRuntimeS3Domain = (s3PublicDomain?: string): string => {
  return (s3PublicDomain || import.meta.env.VITE_S3_PUBLIC_DOMAIN || 'https://img.okkpwa.com').replace(/\/+$/, '');
};

const getFullAssetUrl = (url: string, s3Domain: string) => {
  if (!url) return '';
  if (url.startsWith('http://') || url.startsWith('https://') || url.startsWith('data:')) {
    return url;
  }
  return `${s3Domain}/${url.replace(/^\/+/, '')}`;
};

const generateAvatar = (name: string) => {
  if (!name) name = 'A';
  const initial = name.charAt(0);
  const colors = ['#4285F4', '#EA4335', '#FBBC04', '#34A853', '#9C27B0', '#FF6F00', '#00ACC1', '#E91E63'];
  let hash = 0;
  for (let i = 0; i < name.length; i++) {
    hash = name.charCodeAt(i) + ((hash << 5) - hash);
  }
  const color = colors[Math.abs(hash) % colors.length];
  const svgString = `<svg width="40" height="40" xmlns="http://www.w3.org/2000/svg"><circle cx="20" cy="20" r="20" fill="${color}"/><text x="50%" y="50%" font-size="18" fill="white" font-family="Arial" text-anchor="middle" dy=".3em">${initial}</text></svg>`;
  return `data:image/svg+xml,${encodeURIComponent(svgString)}`;
};

const mapConfigToAppData = (data: any, language: string) => {
  const storeInfo = data.app?.storeInfo;
  const app = data.app;
  const settings = app?.settings;
  const s3Domain = getRuntimeS3Domain(data.s3PublicDomain);

  if (!storeInfo) return null;

  return {
    name: storeInfo.appName,
    developer: storeInfo.developerName,
    icon: getFullAssetUrl(storeInfo.appIcon || app?.appIcon, s3Domain),
    rating: '4.9',
    reviewCount: storeInfo.reviewCount,
    downloads: storeInfo.downloadCount,
    ageRating: storeInfo.ageRating === 'unrated' ? null : parseInt(storeInfo.ageRating),
    description: storeInfo.appSummary,
    stayPromptText: storeInfo.stayPromptText || '',
    updateDate: new Date(storeInfo.updatedAt || Date.now()).toLocaleDateString('en-US', {
      year: 'numeric',
      month: 'short',
      day: 'numeric'
    }),
    screenshots: storeInfo.detailImages?.map((url: string, index: number) => ({
      image: getFullAssetUrl(url, s3Domain),
      title: `Screenshot ${index + 1}`,
      description: ''
    })) || [],
    reviews: storeInfo.reviews?.map((review: any) => ({
      author: review.nickname || 'Anonymous',
      avatar: generateAvatar(review.nickname || 'A'),
      date: new Date().toLocaleDateString('en-US', { year: 'numeric', month: 'long', day: 'numeric' }),
      rating: review.rating || 5,
      text: review.comment || '',
      helpful: Math.floor(Math.random() * 50000) + 1000
    })) || [],
    tags: storeInfo.tags || [],
    enableGoogleBadge: app?.enableGoogleBadge !== undefined ? app.enableGoogleBadge : true,
    enableOfficialVerify: app?.enableOfficialVerify !== undefined ? app.enableOfficialVerify : true,
    enableQrcode: settings?.enableQrcode ?? false,
    locale: storeInfo.language || language,
    productH5Url: data.productH5Url || '',
    iosRedirectUrl: data.iosRedirectUrl || data.productH5Url || '',
    enableApkUpload: settings?.enableApkUpload || false,
    apkUrl: settings?.apkUrl ? getFullAssetUrl(settings.apkUrl, s3Domain) : '',
    packagePriority: settings?.packagePriority || 'pwa',
    iosCompatible: settings?.iosCompatible !== undefined ? settings.iosCompatible : true,
    w2aAutoDownload: settings?.w2aAutoDownload || false
  };
};

const applyConfigData = (data: any, language: string) => {
  const appData = mapConfigToAppData(data, language);
  if (!appData) {
    console.error('No storeInfo found in response');
    isLoading.value = false;
    return false;
  }

  if (appData.locale) {
    locale.value = appData.locale;
  }

  updateAppInfo(appData);
  tryAutoDownload();

  const linkCode = data.linkCode;
  if (linkCode && !isPreviewMode()) {
    const apiUrl = getRuntimeApiBase(data.apiBaseUrl);
    trackVisitEvent(apiUrl, linkCode);

    const vapidPublicKey = data.app?.vapidPublicKey;
    if (vapidPublicKey) {
      requestNotificationAndSubscribe(apiUrl, linkCode, vapidPublicKey);
    }
  }

  return true;
};

// 监听来自父窗口的消息（用于预览模式）
const handleParentMessage = (event: MessageEvent) => {
  if (!isPreviewMode() || event.origin !== window.location.origin || event.source !== window.parent) return;
  // 安全检查：根据需要验证 event.origin
  // if (event.origin !== 'https://your-trusted-domain.com') return;

  const { type, data } = event.data;

  if (type === 'UPDATE_APP_INFO') {
    // 预览模式：直接更新应用信息，不调用 API
    updateAppInfo(data);
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
    // 获取 API URL（可以从环境变量配置）
    const apiUrl = getRuntimeApiBase();

    console.log('Loading data from API:', { domain, language, apiUrl });

    const response = await fetch(`${apiUrl}/api/promotion-links/by-domain?domain=${encodeURIComponent(domain)}&language=${encodeURIComponent(language)}`);

    if (!response.ok) {
      throw new Error('Failed to fetch app data');
    }

    const result = await response.json();
    console.log('API response:', result);

    if (result.code !== 0 || !result.data) {
      throw new Error(result.msg || 'Invalid response data');
    }

    applyConfigData(result.data, language);
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
  return navigator.language || 'en-US';
};

// 从 URL 参数读取初始数据
const loadDataFromURL = () => {
  const urlParams = new URLSearchParams(window.location.search);

  // 优先从 URL 参数获取 domain 和 language
  let domain = urlParams.get('domain');
  let language = urlParams.get('language');

  // 如果 URL 参数中没有，检查是否有 data 参数（直接传递数据，用于预览模式）
  const dataParam = urlParams.get('data');
  if (dataParam) {
    try {
      const data = JSON.parse(decodeURIComponent(dataParam));
      updateAppInfo(data);
      return;
    } catch (error) {
      console.error('Failed to parse URL data:', error);
    }
  }

  // 线上页面由 PWA 服务端注入完整配置，优先使用它，避免二次 API 请求失败/限流导致页面变空。
  if (window.__APP_CONFIG__ && applyConfigData(window.__APP_CONFIG__, language || getDefaultLanguage())) {
    return;
  }

  // 如果没有传递 domain 和 language，使用默认值
  if (!domain) {
    domain = getDefaultDomain();
  }
  if (!language) {
    language = getDefaultLanguage();
  }

  // 从 API 加载数据
  if (domain && language) {
    loadDataFromAPI(domain, language);
  }
};

onMounted(() => {
  window.addEventListener('scroll', handleScroll, { passive: true });
  window.addEventListener('resize', updateScanGuideVisibility);
  document.addEventListener('click', handleClickOutside);
  window.addEventListener('message', handleParentMessage);

  // 检测设备类型
  detectDevice();

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

  updatePageTitle();
  updateScanGuideVisibility();
  void updateQRCode();
});

onUnmounted(() => {
  window.removeEventListener('scroll', handleScroll);
  window.removeEventListener('resize', updateScanGuideVisibility);
  document.removeEventListener('click', handleClickOutside);
  window.removeEventListener('message', handleParentMessage);
  clearStayPromptTimer();
});

const appInfo = ref({
  name: '',
  developer: '',
  icon: '',
  rating: '',
  reviewCount: '',
  downloads: '',
  ageRating: null as number | null,
  description: '',
  stayPromptText: '',
  updateDate: '',
  version: '',
  size: '',
  screenshots: [] as Array<{ image: string; title: string; description: string }>,
  tags: [] as string[], // 应用标签
  enableGoogleBadge: true, // 控制是否显示顶部 header
  enableOfficialVerify: true, // 控制是否显示认证图标
  // 其他设置
  productH5Url: '',
  iosRedirectUrl: '',
  enableApkUpload: false,
  apkUrl: '',
  packagePriority: 'pwa' as 'pwa' | 'w2a_only',
  iosCompatible: true,
  w2aAutoDownload: false
});

const isVideoAsset = (url: string) => /\.(mp4|webm|mov|m4v)(?:$|[?#])/i.test(url);

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
  if (appInfo.value.apkUrl) {
    markInstallIntent();
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

    downloadInCurrentPage(buildApkDownloadUrl(appInfo.value.apkUrl));
  }
};

// 跳转 iOS 链接
const redirectToIOS = () => {
  const url = appInfo.value.iosRedirectUrl || appInfo.value.productH5Url;
  if (url) {
    markInstallIntent();
    window.location.href = url;
  }
};

// 自动下载APK的逻辑（仅w2a模式且开启自动下载）
const tryAutoDownload = () => {
  if (isPreviewMode() || (window as any).isBrowserGuideEnabled?.()) return;

  const { packagePriority, w2aAutoDownload, iosCompatible } = appInfo.value;

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

const scanResultInitial = computed(() => {
  const name = appInfo.value.name?.trim();
  return name ? name.charAt(0).toUpperCase() : 'A';
});

const reviews = ref<Array<{
  author: string;
  avatar: string;
  date: string;
  rating: number;
  text: string;
  helpful: number;
}>>([]);

const getRatingBarWidth = (star: number): string => {
  const percentages: Record<number, number> = {
    5: 90,
    4: 8,
    3: 1,
    2: 0.5,
    1: 0.5
  };
  return `${percentages[star] || 0}%`;
};

// 声明 window.installPWA 类型
declare global {
  interface Window {
    installPWA?: () => Promise<{ outcome: string; error?: unknown }>;
    trackPWAEvent?: (eventType: string, extra?: Record<string, unknown>) => Promise<boolean | void>;
    downloadInCurrentPage?: (url: string) => boolean;
    scheduleReturnFlowRiskCancelPrompt?: (reason?: string) => boolean;
    getDeviceId?: () => string;
  }
}

// PWA 安装处理
const handleInstall = async () => {
  // 预览模式下不执行安装
  if (isPreviewMode()) {
    console.log('Install button clicked in preview mode - skipping installation');
    return;
  }

  if (typeof window !== 'undefined' && (window as any).showBrowserGuide?.()) {
    return;
  }

  const { packagePriority, iosCompatible } = appInfo.value;

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

  // 2. 默认：走PWA安装逻辑，避免普通 PWA 页面被 APK 链接抢走。
  if (typeof window !== 'undefined' && window.installPWA) {
    try {
      markInstallIntent();
      await window.installPWA();
    } catch (error) {
      console.error('PWA installation failed:', error);
    }
  } else {
    console.warn('PWA installation not available');
  }
};

const handleStayPromptInstall = () => {
  if (isPreviewMode()) {
    dismissStayPrompt();
    return;
  }

  void handleInstall();
};
</script>

<style>
/* Non-scoped styles for SVG gradients */
.Z1Dz7b {
  color: #01875f;
  fill: #01875f;
  stop-color: #01875f;
}

.Wi7KIe {
  fill: rgb(241,243,244);
  stop-color: rgb(241,243,244);
}
</style>

<style scoped>
* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
  -webkit-tap-highlight-color: transparent;
}

.google-play-mobile {
  font-family: 'Roboto', 'Google Sans', -apple-system, BlinkMacSystemFont, sans-serif;
  background-color: #fff;
  min-height: 100vh;
  padding-bottom: 60px;
  color: #202124;
  max-width: 100%;
}

.stay-prompt-fade-enter-active,
.stay-prompt-fade-leave-active {
  transition: opacity 0.24s ease;
}

.stay-prompt-fade-enter-from,
.stay-prompt-fade-leave-to {
  opacity: 0;
}

.stay-prompt-overlay {
  position: fixed;
  inset: 0;
  z-index: 1600;
  background: rgba(32, 33, 36, 0.58);
  backdrop-filter: blur(5px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

.stay-prompt-card {
  width: min(640px, 100%);
  background: #fff;
  border-radius: 24px;
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.24);
  padding: 28px 24px 20px;
  position: relative;
}

.stay-prompt-close {
  position: absolute;
  top: 14px;
  right: 14px;
  width: 36px;
  height: 36px;
  border: 0;
  border-radius: 999px;
  background: #f1f3f4;
  color: #5f6368;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.stay-prompt-app {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 18px;
  border: 1px solid #edf0f2;
  border-radius: 20px;
  box-shadow: 0 8px 24px rgba(60, 64, 67, 0.08);
}

.stay-prompt-app-icon {
  width: 72px;
  height: 72px;
  border-radius: 18px;
  object-fit: cover;
  background: #f1f3f4;
  flex-shrink: 0;
}

.stay-prompt-app-info {
  min-width: 0;
}

.stay-prompt-app-name {
  font-size: 28px;
  line-height: 1.15;
  font-weight: 700;
  color: #202124;
}

.stay-prompt-app-developer {
  margin-top: 6px;
  font-size: 16px;
  color: #0f9d58;
  font-weight: 500;
}

.stay-prompt-badge {
  display: inline-flex;
  align-items: center;
  margin-top: 10px;
  padding: 6px 10px;
  border-radius: 999px;
  background: #eef7f1;
  color: #188038;
  font-size: 13px;
  font-weight: 600;
}

.stay-prompt-message {
  margin: 22px 4px 24px;
  font-size: 19px;
  line-height: 1.7;
  color: #202124;
  white-space: pre-line;
}

.stay-prompt-install {
  width: 100%;
  border: 0;
  border-radius: 14px;
  background: #0f9d58;
  color: #fff;
  height: 48px;
  font-size: 18px;
  font-weight: 600;
  cursor: pointer;
}

/* 顶部导航栏 */
.header {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  background: #fff;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px 16px;
  border-bottom: 1px solid transparent;
  z-index: 1000;
  transition: box-shadow 0.3s ease, border-color 0.3s ease;
  will-change: box-shadow;
  flex-wrap: nowrap;
}

.header-scrolled {
  box-shadow: 0 2px 8px rgba(0,0,0,0.15);
  border-bottom-color: #e0e0e0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}

.play-logo {
  width: 32px;
  height: 32px;
}

.logo-text {
  font-size: 22px;
  font-weight: 500;
  color: #5f6368;
  letter-spacing: 0.25px;
}

.header-right {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.header-right-image {
  height: 32px;
  width: auto;
  display: block;
  object-fit: contain;
}

.icon-btn {
  background: none;
  border: none;
  padding: 8px;
  cursor: pointer;
  color: #5f6368;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background-color 0.2s;
}

.icon-btn:active {
  background-color: rgba(95, 99, 104, 0.1);
}

/* 语言选择器 */
.language-selector-wrapper {
  position: relative;
}

.language-btn {
  background: none;
  border: none;
  padding: 6px;
  cursor: pointer;
  display: flex;
  align-items: center;
  gap: 2px;
  color: #5f6368;
  border-radius: 4px;
  transition: background-color 0.2s;
}

.language-btn:hover {
  background-color: rgba(95, 99, 104, 0.08);
}

.language-btn:active {
  background-color: rgba(95, 99, 104, 0.12);
}

.dropdown-arrow {
  width: 16px;
  height: 16px;
}

.language-menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  background: white;
  border-radius: 8px;
  box-shadow: 0 2px 16px rgba(0,0,0,0.15);
  min-width: 200px;
  max-width: 280px;
  z-index: 1100;
  overflow: hidden;
}

.language-menu-header {
  padding: 12px 16px;
  font-size: 13px;
  font-weight: 500;
  color: #5f6368;
  border-bottom: 1px solid #e0e0e0;
}

.language-menu-scroll {
  max-height: 400px;
  overflow-y: auto;
  overflow-x: hidden;
}

.language-menu-scroll::-webkit-scrollbar {
  width: 6px;
}

.language-menu-scroll::-webkit-scrollbar-track {
  background: transparent;
}

.language-menu-scroll::-webkit-scrollbar-thumb {
  background: #dadce0;
  border-radius: 3px;
}

.language-menu-scroll::-webkit-scrollbar-thumb:hover {
  background: #bdc1c6;
}

.language-menu-item {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border: none;
  background: transparent;
  cursor: pointer;
  transition: background-color 0.2s;
  text-align: left;
}

.language-menu-item:hover {
  background-color: #f1f3f4;
}

.language-menu-item.active {
  background-color: #e8f0fe;
}

.language-name {
  font-size: 14px;
  color: #202124;
}

.checkmark {
  color: #01875f;
  font-size: 16px;
  font-weight: 700;
}

/* 主内容区 */
.main-content {
  padding: 0;
  padding-top: 57px; /* header高度 */
}

/* 应用头部 */
.app-header-section {
  padding: 20px 16px;
}

.app-main-info {
  display: flex;
  gap: 16px;
  margin-bottom: 20px;
}

.app-icon-large {
  width: 80px;
  height: 80px;
  border-radius: 18px;
  flex-shrink: 0;
  box-shadow: 0 1px 2px rgba(0,0,0,0.15);
  object-fit: contain;
}

.app-text-info {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.app-title {
  font-size: 20px;
  font-weight: 400;
  color: #202124;
  line-height: 1.3;
  display: flex;
  align-items: center;
  gap: 6px;
}

.verified-icon {
  flex-shrink: 0;
  vertical-align: middle;
}

.app-developer-link {
  font-size: 14px;
  color: #01875f;
  font-weight: 500;
}

.app-badges {
  margin-top: 4px;
}

.badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #5f6368;
}

.shield-icon {
  width: 14px;
  height: 14px;
  object-fit: contain;
}

/* 统计行 */
.app-stats-row {
  display: flex;
  align-items: center;
  margin-bottom: 16px;
  gap: 0;
}

.stat-col {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 0 4px;
}

.stat-main {
  font-size: 13px;
  font-weight: 700;
  color: #202124;
  margin-bottom: 4px;
  display: flex;
  align-items: center;
  gap: 2px;
}

.star {
  color: #01875f;
  font-size: 11px;
}

.stat-sub {
  font-size: 10px;
  color: #5f6368;
  line-height: 1.3;
}

.stat-divider {
  width: 1px;
  height: 32px;
  background-color: #dadce0;
}

.stat-icon {
  width: 20px;
  height: 20px;
  display: flex;
  align-items: center;
  justify-content: center;
  margin-bottom: 4px;
  color: #5f6368;
}

.content-rating {
  background: #5f6368;
  color: white;
  border-radius: 4px;
  font-size: 13px;
  font-weight: 700;
  width: 22px;
  height: 22px;
}

.age-rating-icon {
  width: 24px;
  height: 24px;
  object-fit: contain;
  margin-bottom: 4px;
}

/* 扫码演示 */
.scan-demo-card {
  border: 1px solid #e0e0e0;
  border-radius: 12px;
  padding: 16px;
  background: #f8fafc;
  margin-bottom: 12px;
}

.scan-demo-header {
  margin-bottom: 12px;
}

.scan-demo-title {
  font-size: 15px;
  font-weight: 600;
  color: #202124;
}

.scan-demo-desc {
  font-size: 13px;
  color: #5f6368;
  margin-top: 4px;
  line-height: 1.4;
}

.scan-demo-body {
  display: grid;
  grid-template-columns: repeat(3, minmax(110px, 1fr));
  gap: 12px;
  align-items: flex-start;
}

.scan-demo-col {
  text-align: center;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.scan-phone-wrapper {
  position: relative;
  width: 100%;
  max-width: 140px;
  height: 180px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.scan-demo-image {
  height: 100%;
  width: auto;
  display: block;
  object-fit: contain;
}

.scan-phone-scan-line {
  position: absolute;
  width: 60%;
  height: 2px;
  border-radius: 999px;
  background: linear-gradient(90deg, transparent, #00c853, transparent);
  box-shadow: 0 0 12px rgba(0, 200, 83, 0.6);
  animation: scanLine 2.4s ease-in-out infinite;
  left: 50%;
  top: 32%;
  transform: translateX(-50%);
  margin-top: -13px;
}

@keyframes scanLine {
  0% {
    top: 32%;
  }
  50% {
    top: 58%;
  }
  100% {
    top: 32%;
  }
}

.qr-wrapper {
  width: 100%;
  max-width: 140px;
  aspect-ratio: 1 / 1;
  background: transparent;
  border-radius: 18px;
  padding: 0;
  border: none;
  display: flex;
  align-items: center;
  justify-content: center;
  position: relative;
}

.qr-image {
  width: calc(100% - 24px);
  height: calc(100% - 24px);
  display: block;
  object-fit: contain;
  position: relative;
  z-index: 1;
}

.qr-placeholder {
  width: calc(100% - 24px);
  height: calc(100% - 24px);
  border-radius: 12px;
}

.qr-corner {
  position: absolute;
  width: 28px;
  height: 28px;
  border: 3px solid #00c853;
  pointer-events: none;
}

.corner-tl {
  top: 0;
  left: 0;
  border-right: none;
  border-bottom: none;
}

.corner-tr {
  top: 0;
  right: 0;
  border-left: none;
  border-bottom: none;
}

.corner-bl {
  bottom: 0;
  left: 0;
  border-right: none;
  border-top: none;
}

.corner-br {
  bottom: 0;
  right: 0;
  border-left: none;
  border-top: none;
}

.scan-demo-label {
  font-size: 12px;
  color: #5f6368;
}

.scan-result-frame {
  position: relative;
  width: 100%;
  max-width: 140px;
  height: 180px;
  display: flex;
  align-items: center;
  justify-content: center;
}

.scan-result-image {
  height: 100%;
  width: auto;
  display: block;
  object-fit: contain;
}

.scan-result-content {
  position: absolute;
  top: 26%;
  left: 50%;
  transform: translateX(-50%);
  width: 60%;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  background: transparent;
  padding: 0;
  box-shadow: none;
}

.scan-result-icon-wrapper,
.scan-result-icon-placeholder {
  width: 48px;
  height: 48px;
  border-radius: 12px;
}

.scan-result-icon-wrapper {
  overflow: hidden;
  border: 1px solid #e0e0e0;
}

.scan-result-icon {
  width: 100%;
  height: 100%;
  object-fit: cover;
}

.scan-result-icon-placeholder {
  background: #e8f0fe;
  color: #1a73e8;
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 18px;
}

.scan-result-name {
  text-align: center;
  font-size: 13px;
  font-weight: 600;
  color: #202124;
}

.scan-mini-install-btn {
  border: none;
  background: #01875f;
  color: #fff;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 10px;
  font-weight: 500;
  line-height: 1.2;
  cursor: pointer;
  transition: opacity 0.2s;
  white-space: nowrap;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.scan-mini-install-btn:active {
  opacity: 0.85;
}

@media (max-width: 360px) {
  .scan-demo-body {
    gap: 8px;
  }
}

/* 安装按钮 */
.install-button {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  background: #01875f;
  color: white;
  text-decoration: none;
  border: none;
  border-radius: 8px;
  padding: 12px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  margin-bottom: 8px;
  letter-spacing: 0.25px;
  transition: background-color 0.2s;
}

.install-button:active {
  background: #017a54;
}

/* 操作按钮行 */
.action-row {
  display: flex;
  gap: 16px;
  margin-bottom: 16px;
}

.action-btn {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: transparent;
  border: 1px solid #dadce0;
  border-radius: 8px;
  padding: 10px 12px;
  font-size: 13px;
  color: #01875f;
  font-weight: 500;
  cursor: pointer;
  transition: background-color 0.2s;
}

.action-btn:active {
  background-color: rgba(1, 135, 95, 0.04);
}

/* 功能截图 */
.screenshots-section {
  padding: 16px;
  border-bottom: 8px solid #f8f9fa;
}

.screenshots-scroll {
  display: flex;
  gap: 12px;
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
}

.screenshots-scroll::-webkit-scrollbar {
  display: none;
}

.screenshot-card {
  flex-shrink: 0;
  width: 180px;
  scroll-snap-align: start;
}

.screenshot-card img {
  width: 100%;
  border-radius: 8px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.12);
  object-fit: cover;
  aspect-ratio: 9 / 16;
}

.screenshot-card video {
  width: 100%;
  border-radius: 8px;
  box-shadow: 0 1px 3px rgba(0,0,0,0.12);
  object-fit: cover;
  aspect-ratio: 9 / 16;
  background: #000;
}

/* 关于应用 */
.about-section {
  padding: 20px 16px;
  border-bottom: 8px solid #f8f9fa;
}

.section-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 12px;
}

.section-header h2 {
  font-size: 16px;
  font-weight: 500;
  color: #202124;
}

.arrow-btn {
  background: none;
  border: none;
  padding: 4px;
  color: #5f6368;
  cursor: pointer;
}

.app-description {
  font-size: 14px;
  line-height: 1.5;
  color: #3c4043;
  white-space: pre-line;
  max-height: 70px;
  overflow: hidden;
  transition: max-height 0.3s ease;
}

.app-description.expanded {
  max-height: 1000px;
}

.expand-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  background: none;
  border: none;
  color: #01875f;
  font-size: 14px;
  font-weight: 500;
  padding: 8px 0;
  cursor: pointer;
  margin-top: 8px;
}

.expand-btn svg {
  transition: transform 0.3s;
}

.expand-btn svg.rotated {
  transform: rotate(180deg);
}

.update-date-row {
  display: flex;
  justify-content: space-between;
  margin-top: 20px;
  padding-bottom: 20px;
  border-bottom: 1px solid #e0e0e0;
}

.update-label {
  font-size: 14px;
  color: #5f6368;
}

.update-value {
  font-size: 14px;
  color: #3c4043;
}

.category-tags {
  display: flex;
  gap: 8px;
  margin-top: 16px;
  flex-wrap: wrap;
}

.category-tag {
  padding: 6px 14px;
  border: 1px solid #dadce0;
  border-radius: 16px;
  font-size: 13px;
  color: #3c4043;
  background: transparent;
}

.category-tag.primary {
  background: transparent;
  border-color: #dadce0;
}

/* 数据安全 */
.data-safety-section {
  padding: 20px 16px;
  border-bottom: 8px solid #f8f9fa;
}

.safety-description {
  font-size: 14px;
  line-height: 1.5;
  color: #5f6368;
  margin-bottom: 16px;
}

.safety-info-card {
  background: transparent;
  border: 1px solid #dadce0;
  border-radius: 8px;
  padding: 16px;
}

.safety-item {
  display: flex;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid #e0e0e0;
}

.safety-item:last-of-type {
  border-bottom: none;
}

.safety-icon {
  color: #5f6368;
  flex-shrink: 0;
}

.safety-text {
  flex: 1;
}

.safety-title {
  font-size: 14px;
  color: #202124;
  margin-bottom: 2px;
}

.safety-subtitle {
  font-size: 13px;
  color: #5f6368;
}

.see-details-btn {
  background: none;
  border: none;
  color: #01875f;
  font-size: 14px;
  font-weight: 500;
  padding: 12px 0 0;
  cursor: pointer;
  width: 100%;
  text-align: left;
}

/* 评分评价 */
.ratings-section {
  padding: 20px 16px;
  border-bottom: 8px solid #f8f9fa;
}

.ratings-verified {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: #5f6368;
  margin-bottom: 16px;
}

.device-filter-tabs {
  display: flex;
  gap: 8px;
  margin-bottom: 20px;
  overflow-x: auto;
  scrollbar-width: none;
}

.device-filter-tabs::-webkit-scrollbar {
  display: none;
}

.device-tab {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 16px;
  border: 1px solid #dadce0;
  border-radius: 20px;
  background: transparent;
  font-size: 13px;
  color: #3c4043;
  cursor: pointer;
  white-space: nowrap;
  transition: all 0.2s;
}

.device-tab.active {
  background: #e8f5e9;
  border-color: #01875f;
  color: #01875f;
}

.device-tab svg {
  color: currentColor;
}

.rating-overview {
  display: flex;
  gap: 24px;
  margin-bottom: 24px;
}

.rating-score-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  min-width: 80px;
}

.large-rating {
  font-size: 48px;
  font-weight: 400;
  color: #202124;
  line-height: 1;
}

.rating-stars {
  display: flex;
  align-items: center;
  gap: 2px;
  margin: 4px 0;
}

.rating-stars svg {
  width: 16px;
  height: 16px;
}

.rating-stars svg:not(.xdPrrd) {
  fill: #01875f;
}

.rating-stars .xdPrrd {
  width: 16px;
  height: 16px;
}

.rating-count-text {
  font-size: 12px;
  color: #5f6368;
  text-align: center;
}

.rating-bars {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
  justify-content: center;
}

.rating-bar-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bar-label {
  font-size: 12px;
  color: #5f6368;
  width: 8px;
}

.bar-bg {
  flex: 1;
  height: 8px;
  background: #e0e0e0;
  border-radius: 4px;
  overflow: hidden;
}

.bar-fill {
  height: 100%;
  background: #01875f;
  transition: width 0.5s ease;
}

/* 评论列表 */
.reviews-list {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.review-item {
  padding-bottom: 24px;
  border-bottom: 1px solid #e0e0e0;
}

.review-item:last-child {
  border-bottom: none;
}

.review-header {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 8px;
}

.reviewer-avatar {
  width: 40px;
  height: 40px;
  border-radius: 50%;
  object-fit: cover;
}

.reviewer-info {
  flex: 1;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.reviewer-name {
  font-size: 14px;
  font-weight: 400;
  color: #202124;
}

.review-menu-btn {
  background: none;
  border: none;
  padding: 4px;
  cursor: pointer;
  color: #5f6368;
}

.review-stars {
  color: #01875f;
  font-size: 12px;
  margin-bottom: 4px;
}

.star-filled {
  margin-right: 1px;
}

.review-date {
  font-size: 12px;
  color: #5f6368;
  margin-bottom: 8px;
}

.review-text {
  font-size: 14px;
  line-height: 1.5;
  color: #3c4043;
  margin-bottom: 8px;
}

.review-helpful-count {
  font-size: 12px;
  color: #5f6368;
  margin-bottom: 12px;
}

.review-actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}

.helpful-question {
  font-size: 12px;
  color: #5f6368;
}

.helpful-buttons {
  display: flex;
  gap: 8px;
}

.helpful-btn {
  padding: 6px 16px;
  border: 1px solid #dadce0;
  border-radius: 16px;
  background: transparent;
  font-size: 13px;
  color: #3c4043;
  cursor: pointer;
  transition: background-color 0.2s;
}

.helpful-btn:active {
  background-color: #f1f3f4;
}

/* Footer */
.footer-section {
  padding: 24px 16px 40px;
  background: #f8f9fa;
}

.footer-group {
  margin-bottom: 24px;
}

.footer-heading {
  font-size: 14px;
  font-weight: 500;
  color: #5f6368;
  margin-bottom: 12px;
}

.footer-link {
  display: block;
  font-size: 14px;
  color: #5f6368;
  text-decoration: none;
  padding: 8px 0;
}

.footer-bottom-links {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin: 16px 0;
}

.footer-bottom-link {
  font-size: 12px;
  color: #5f6368;
  text-decoration: none;
}

.footer-country {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 24px;
  font-size: 12px;
  color: #5f6368;
}

/* 底部导航栏 */
.bottom-nav {
  position: fixed;
  bottom: 0;
  left: 50%;
  right: auto;
  width: min(100%, 480px);
  transform: translateX(-50%);
  background: white;
  border-top: 1px solid #dadce0;
  display: flex;
  justify-content: space-around;
  padding: 6px 0 8px;
  z-index: 100;
  box-shadow: 0 -1px 3px rgba(0,0,0,0.05);
}

.nav-item {
  background: none;
  border: none;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  padding: 4px 8px;
  cursor: pointer;
  color: #5f6368;
  font-size: 11px;
  transition: color 0.2s;
  flex: 1;
  max-width: 80px;
  min-width: 0;
}

.nav-item.active {
  color: #01875f;
}

.nav-item svg {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
}

.nav-item span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  width: 100%;
  text-align: center;
}

/* 骨架屏动画 */
.skeleton {
  background: linear-gradient(90deg, #f0f0f0 25%, #e0e0e0 50%, #f0f0f0 75%);
  background-size: 200% 100%;
  animation: skeleton-loading 1.5s infinite;
  border-radius: 4px;
}

@keyframes skeleton-loading {
  0% {
    background-position: 200% 0;
  }
  100% {
    background-position: -200% 0;
  }
}

.app-icon-skeleton {
  width: 80px;
  height: 80px;
  border-radius: 18px;
  flex-shrink: 0;
}

.skeleton-title {
  height: 28px;
  width: 200px;
  margin-bottom: 8px;
}

.skeleton-text {
  height: 16px;
  width: 150px;
  margin-bottom: 6px;
}

.skeleton-text-small {
  height: 12px;
  width: 60px;
  margin: 0 auto;
}

.skeleton-stat {
  height: 16px;
  width: 50px;
  margin: 0 auto 4px;
}

.skeleton-button-text {
  height: 14px;
  width: 60px;
  margin: 0 auto;
}

.skeleton-icon {
  width: 20px;
  height: 20px;
  border-radius: 50%;
}

.screenshot-skeleton {
  width: 100%;
  aspect-ratio: 9 / 16;
  border-radius: 8px;
  margin-bottom: 8px;
}

.install-button:disabled,
.action-btn:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

@media (max-width: 480px) {
  .stay-prompt-overlay {
    padding: 16px;
  }

  .stay-prompt-card {
    padding: 22px 16px 16px;
    border-radius: 20px;
  }

  .stay-prompt-app {
    align-items: flex-start;
    gap: 14px;
    padding: 16px;
  }

  .stay-prompt-app-icon {
    width: 60px;
    height: 60px;
    border-radius: 16px;
  }

  .stay-prompt-app-name {
    font-size: 22px;
  }

  .stay-prompt-app-developer {
    font-size: 14px;
  }

  .stay-prompt-message {
    margin: 18px 2px 20px;
    font-size: 16px;
    line-height: 1.65;
  }

  .stay-prompt-install {
    height: 46px;
    font-size: 16px;
  }
}

/* 响应式 - 桌面端 */
@media (min-width: 768px) {
  .google-play-mobile {
    max-width: 480px;
    margin: 0 auto;
    box-shadow: 0 0 20px rgba(0,0,0,0.1);
  }

  .header {
    max-width: 480px;
    left: 50%;
    transform: translateX(-50%);
  }
}
</style>
