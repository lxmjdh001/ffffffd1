import { api } from './api';
import { readPagePicture } from './page-images';

export async function uploadPagePicture(appID: string, file: File, icon: boolean): Promise<string> {
  const processed = await readPagePicture(file, icon);
  // Do not fetch(data:) or use blob: image URLs: neither is needed under D1's
  // strict CSP. Only the processed bytes are sent to our same-origin API.
  const encoded = processed.split(',')[1];
  const bytes = Uint8Array.from(atob(encoded), character => character.charCodeAt(0));
  const form = new FormData();
  form.append('file', new Blob([bytes], { type: 'image/jpeg' }), 'page-image.jpg');
  const out = await api<{ url: string }>(`/apps/${encodeURIComponent(appID)}/page-images`, { method: 'POST', body: form });
  return out.url;
}
