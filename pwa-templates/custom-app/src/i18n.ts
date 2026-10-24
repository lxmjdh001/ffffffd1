import { createI18n } from 'vue-i18n';

// 语言映射
export const languageMap: Record<string, string> = {
  'en-US': 'English',
  'zh-CN': '简体中文',
  'zh-TW': '繁體中文',
  'ja-JP': '日本語',
  'ko-KR': '한국어',
  'es-ES': 'Español',
  'fr-FR': 'Français',
  'de-DE': 'Deutsch',
  'it-IT': 'Italiano',
  'pt-PT': 'Português',
  'ru-RU': 'Русский',
  'ar-SA': 'العربية',
  'th-TH': 'ไทย',
  'vi-VN': 'Tiếng Việt',
  'id-ID': 'Bahasa Indonesia',
  'ms-MY': 'Bahasa Melayu',
  'tr-TR': 'Türkçe',
  'pl-PL': 'Polski',
  'nl-NL': 'Nederlands',
  'sv-SE': 'Svenska'
};

const messages = {
  'en-US': {
    install: 'Install',
    loading: 'Loading...',
    scanToVisit: 'Scan to Visit'
  },
  'zh-CN': {
    install: '安装',
    loading: '加载中...',
    scanToVisit: '扫码访问'
  },
  'zh-TW': {
    install: '安裝',
    loading: '載入中...',
    scanToVisit: '掃碼訪問'
  },
  'ja-JP': {
    install: 'インストール',
    loading: '読み込み中...',
    scanToVisit: 'スキャンして訪問'
  },
  'ko-KR': {
    install: '설치',
    loading: '로딩 중...',
    scanToVisit: 'QR 코드 스캔'
  },
  'es-ES': {
    install: 'Instalar',
    loading: 'Cargando...',
    scanToVisit: 'Escanear para visitar'
  },
  'fr-FR': {
    install: 'Installer',
    loading: 'Chargement...',
    scanToVisit: 'Scanner pour visiter'
  },
  'de-DE': {
    install: 'Installieren',
    loading: 'Lädt...',
    scanToVisit: 'Scannen zum Besuchen'
  },
  'it-IT': {
    install: 'Installa',
    loading: 'Caricamento...',
    scanToVisit: 'Scansiona per visitare'
  },
  'pt-PT': {
    install: 'Instalar',
    loading: 'Carregando...',
    scanToVisit: 'Digitalize para visitar'
  },
  'ru-RU': {
    install: 'Установить',
    loading: 'Загрузка...',
    scanToVisit: 'Сканировать для посещения'
  },
  'ar-SA': {
    install: 'تثبيت',
    loading: 'جار التحميل...',
    scanToVisit: 'امسح للزيارة'
  },
  'th-TH': {
    install: 'ติดตั้ง',
    loading: 'กำลังโหลด...',
    scanToVisit: 'สแกนเพื่อเยี่ยมชม'
  },
  'vi-VN': {
    install: 'Cài đặt',
    loading: 'Đang tải...',
    scanToVisit: 'Quét để truy cập'
  },
  'id-ID': {
    install: 'Pasang',
    loading: 'Memuat...',
    scanToVisit: 'Pindai untuk mengunjungi'
  },
  'ms-MY': {
    install: 'Pasang',
    loading: 'Memuatkan...',
    scanToVisit: 'Imbas untuk melawat'
  },
  'tr-TR': {
    install: 'Yükle',
    loading: 'Yükleniyor...',
    scanToVisit: 'Ziyaret etmek için tara'
  },
  'pl-PL': {
    install: 'Zainstaluj',
    loading: 'Ładowanie...',
    scanToVisit: 'Skanuj, aby odwiedzić'
  },
  'nl-NL': {
    install: 'Installeren',
    loading: 'Laden...',
    scanToVisit: 'Scan om te bezoeken'
  },
  'sv-SE': {
    install: 'Installera',
    loading: 'Laddar...',
    scanToVisit: 'Skanna för att besöka'
  }
};

// 获取默认语言
const getDefaultLocale = (): string => {
  if (typeof navigator === 'undefined') return 'en-US';

  const browserLang = navigator.language;

  // 如果浏览器语言在支持的语言列表中,直接使用
  if (messages[browserLang as keyof typeof messages]) {
    return browserLang;
  }

  // 尝试匹配语言代码的前半部分 (例如 en-GB -> en-US)
  const langPrefix = browserLang.split('-')[0];
  const matchedLang = Object.keys(messages).find(lang => lang.startsWith(langPrefix));

  return matchedLang || 'en-US';
};

export const i18n = createI18n({
  legacy: false,
  locale: getDefaultLocale(),
  fallbackLocale: 'en-US',
  messages
});
