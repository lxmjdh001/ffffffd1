import { t } from './i18n';

export function getDIYPrimaryButtonExample(): string {
  return [
    '<button type="button" data-d1-action="primary">',
    `  <span data-d1-bind="primary-label">${t('立即安装')}</span>`,
    '</button>',
  ].join('\n');
}

// Keep the platform's functional HTML attributes outside translated prose.
// This prompt is copied locally; it is never sent to an AI service by D1.
export function getDIYLandingAIPrompt(): string {
  const requirements = [
    t('只交付一个完整、可直接运行的 HTML 文件。CSS 写在 style 中，需要的轻量原生 JavaScript 写在 script 中；不要输出项目脚手架、npm 依赖、需要构建的框架代码或 Markdown 代码围栏。'),
    t('包含完整的 html、head、body、网页 title 和移动端 viewport。首屏内容默认可见，优先设计手机效果，同时适配桌面。'),
    t('将页面大标题和正文直接写在 h1/h2、p 等 HTML 元素中，保留可编辑的真实文字；不要通过脚本动态拼接关键文案，也不要把文字画进图片、Canvas 或 CSS 伪元素。'),
    t('图片使用独立的 img 元素或 CSS 背景图，素材地址使用公开 HTTPS 图片直链，避免登录或跳转链接，便于之后在平台替换图片。不要把整个页面做成一张图片。'),
    t('为标题、段落和图片添加易读的 {0} 属性，例如主标题、介绍文案、主视觉图片。', { 0: 'data-d1-label' }),
    t('所有下载或安装主按钮都必须使用下方的按钮结构。图标放在按钮文字 span 外；颜色、尺寸和布局可以自由设计；有多个主按钮时，每个都保留相同的平台标记。'),
    t('不要为主按钮写死下载地址、应用 ID、onclick 跳转或安装、计费接口。按钮实际操作由平台中选择绑定的 APK 或 PWA 决定。'),
    t('辅助导航使用页内锚点，问答可使用 details/summary。除下载或安装主按钮外，不要创建其他 button 或 role="button" 控件，防止普通交互被识别为主操作。'),
    t('不要使用 iframe、表单提交、自动跳转、window.open、自定义 manifest 或 Service Worker；不要自行申请安装或通知权限。'),
    t('不要依赖 fetch/AJAX、外部模块或后端服务获取关键内容。需要的文案和布局应直接包含在 HTML 中，动效不要重建可编辑的正文。'),
  ];
  return [
    t('请为我设计一个适配 D1 Apk 平台的应用推广落地页。'),
    '',
    t('应用或品牌名称：【填写名称】'),
    t('页面语言：【填写语言】'),
    t('目标用户与主要卖点：【填写需求】'),
    t('视觉风格、配色和图片要求：【填写需求】'),
    '',
    t('请严格遵守以下平台规范：'),
    ...requirements.map((requirement, index) => `${index + 1}. ${requirement}`),
    '',
    t('下载或安装主按钮的必需结构：'),
    getDIYPrimaryButtonExample(),
    '',
    t('交付前检查上述要求，只输出完整 HTML，不附加解释。'),
  ].join('\n');
}

export function getDIYLandingHelpSteps(): { title: string; body: string }[] {
  return [
    {
      title: t('让 AI 设计页面'),
      body: t('复制下方提示词，填写应用名称、语言和设计需求，发给你常用的 AI 工具，获取完整 HTML。'),
    },
    {
      title: t('粘贴、预览并发布'),
      body: t('打开「落地页 → DIY落地页」，粘贴 HTML，检查右侧效果和主按钮，保存并发布。复制时不要带 AI 的解释文字或 Markdown 代码围栏。'),
    },
    {
      title: t('选择应用并编辑内容'),
      body: t('在 APK/PWA 新建或编辑页选择 DIY落地页，再选择自己的已发布页面或收藏模版。修改网页标题、正文、图片和按钮文案后，保存当前应用即可使用。'),
    },
  ];
}

export function getDIYLandingHelpQuestions(): { question: string; paragraphs: string[] }[] {
  return [
    {
      question: t('AI 生成的落地页都能直接使用吗？'),
      paragraphs: [
        t('完整的静态 HTML、内联样式和轻量原生脚本最适合直接粘贴。页面风格可以自由设计，但需要保留平台主按钮标记。复制提示词后，先把【】中的内容改成自己的需求。'),
        t('React/Vue 项目源码、需要 npm 构建的工程、嵌套网页和依赖后台接口的页面不能直接当作单文件落地页使用。可以让 AI 按提示词改成静态 HTML。'),
      ],
    },
    {
      question: t('怎样让主按钮正确关联 APK 或 PWA？'),
      paragraphs: [
        t('使用示例中的 {0} 标记主按钮，并将按钮文字放在 {1} 内。多个下载或安装主按钮都加上这些标记。', { 0: 'data-d1-action="primary"', 1: 'data-d1-bind="primary-label"' }),
        t('无需在 AI 代码里填写应用下载地址。平台按当前绑定的 APK/PWA 处理下载、安装、外部跳转、回流和计费；不要让 AI 另外编写这些接口。'),
        t('辅助导航使用页内链接，问答使用 details/summary 等原生结构。普通关闭、轮播或表单按钮可能被当作主操作，建议让 AI 改为静态展示或页内导航。'),
      ],
    },
    {
      question: t('为什么预览里不能真正下载或安装？'),
      paragraphs: [
        t('预览只检查页面效果和按钮接入，不会下载、安装、外跳或产生计费。保存应用后，可打开公开页面验证完整流程。'),
        t('当前公开页面点击自定义主按钮后，会先显示平台确认按钮；访客还需再点击该按钮，才执行当前应用的下载、安装或已配置外跳。这一步保留浏览器所需的真实点击操作。'),
      ],
    },
    {
      question: t('标题、文案和按钮文字会自动识别吗？'),
      paragraphs: [
        t('平台会识别网页 title、可见标题、正文、图片和常见主按钮，网页标题与页面内的大标题分别编辑。推荐使用 h1/h2、p、img 等清晰结构；data-d1-label 用于命名编辑字段，不是模板变量。'),
        t('不能保证所有 AI 页面都能完整识别。脚本动态生成的内容、CSS 伪元素文案，以及图片、SVG 或 Canvas 中的文字无法作为普通字段直接编辑，请让 AI 将它们改成 HTML 文字。'),
        t('若主按钮没有识别到，优先让 AI 补齐示例标记，也可以在 DIY 工作台填写主按钮选择器，例如 #download 或 .install-button。'),
      ],
    },
    {
      question: t('图片怎样上传，为什么有的图片不显示？'),
      paragraphs: [
        t('在当前应用的「编辑页面内容」中替换图片，新图片会上传到 R2，应用只保存素材引用。支持 PNG、JPG、GIF 和 WebP，单图不超过 2 MB、边长 4096 像素且不超过 1600 万像素；上传失败会保留原图，替换后需要保存应用。若提示 R2 未配置，请联系管理员。'),
        t('外部图片需要公开 HTTPS 图片直链。AI 使用相对资源路径时，需在 DIY 工作台填写 HTTPS 资源基址；需要登录、内网地址、跳转链接或无法公开访问的素材可能无法显示。'),
      ],
    },
    {
      question: t('修改内容会影响原模版或其他应用吗？'),
      paragraphs: [
        t('应用里的内容修改仅用于当前 APK/PWA，原 DIY 页面、收藏模版和其他应用保持原样。每个字段可以恢复原始内容；在当前应用中改选其他页面或其他已发布版本时，会清空原版本的专属修改。仅发布新版不会自动替换已有绑定。'),
      ],
    },
  ];
}
