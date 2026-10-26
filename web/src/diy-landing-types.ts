export type DIYLandingKind = 'apk' | 'pwa';
export type DIYLandingContent = Record<string, string>;
export type DIYLandingReference = { page_id: string; revision_id: string; content?: DIYLandingContent };
export type DIYLandingContentField = {
  id: string; kind: 'title' | 'text' | 'image' | 'button'; label: string; value: string; max_length?: number; preview_url?: string;
};
export type DIYLandingContentSchema = {
  fields: DIYLandingContentField[]; max_image_bytes?: number; max_content_bytes?: number;
};
export type DIYLandingImageUpload = {
  id: string; url: string; reference: string; preview_url: string; content_type: string; size: number;
};
export type DIYLandingOptions = {
  targets: DIYLandingKind[];
  primary_selector: string;
  platform_button: boolean;
  resource_base: string;
};
export type DIYHTMLIssue = { code?: string; message: string; severity?: string; line?: number };
export type DIYLandingRevision = {
  id: string; page_id: string; revision_no: number; raw_html?: string; render_html?: string;
  config: DIYLandingOptions; content_hash?: string; published_at: string;
};
export type DIYLandingPageSummary = {
  id: string; name: string; draft_config: DIYLandingOptions; draft_version: number;
  latest_published_revision_id?: string; latest_revision_no?: number; binding_count?: number;
  archived_at?: string | null; created_at: string; updated_at: string;
};
export type DIYLandingPage = DIYLandingPageSummary & { draft_html: string; revisions?: DIYLandingRevision[] };
export type DIYLandingBinding = {
  resource_type: DIYLandingKind; resource_id: string; revision_id: string;
  name?: string; resource_name?: string; revision_no?: number;
};
export const defaultDIYLandingOptions = (): DIYLandingOptions => ({
  targets: ['apk', 'pwa'], primary_selector: '', platform_button: true, resource_base: '',
});
export const normalizeDIYLandingOptions = (value?: Partial<DIYLandingOptions>): DIYLandingOptions => ({
  ...defaultDIYLandingOptions(), ...value,
  targets: value?.targets?.filter(kind => kind === 'apk' || kind === 'pwa') || ['apk', 'pwa'],
});
