const maxInputBytes = 8 * 1024 * 1024;
const maxOutputLength = 120000;
const formats = new Set(['image/png', 'image/jpeg', 'image/webp']);

function readDataURL(file: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => typeof reader.result === 'string' ? resolve(reader.result) : reject(new Error('图片读取失败，请重新选择文件'));
    reader.onerror = () => reject(new Error('图片读取失败，请重新选择文件'));
    reader.onabort = () => reject(new Error('图片读取已取消，请重新选择文件'));
    reader.readAsDataURL(file);
  });
}

export async function readPagePicture(file: File, icon = false): Promise<string> {
  const extension = file.name.split('.').pop()?.toLowerCase();
  const inferred = extension === 'png' ? 'image/png' : extension === 'webp' ? 'image/webp' : ['jpg', 'jpeg'].includes(extension || '') ? 'image/jpeg' : '';
  const mime = file.type.toLowerCase().replace(/^image\/jpg$/, 'image/jpeg') || inferred;
  if (!formats.has(mime)) throw new Error('请选择 PNG、JPEG 或 WebP 图片；HEIC 图片请先转换格式');
  if (file.size === 0) throw new Error('图片文件为空，请重新选择');
  if (file.size > maxInputBytes) throw new Error('单张原图不能超过 8 MB');

  // D1 permits data: images but deliberately does not permit blob: images.
  // Read the local file without changing the site's content security policy.
  const source = await readDataURL(file.type === mime ? file : file.slice(0, file.size, mime));
  const image = await new Promise<HTMLImageElement>((resolve, reject) => {
    const picture = new Image();
    picture.onload = () => resolve(picture);
    picture.onerror = () => reject(new Error('图片无法读取，请确认文件未损坏，并重新选择 PNG、JPEG 或 WebP 图片'));
    picture.src = source;
  });
  if (!image.naturalWidth || !image.naturalHeight) throw new Error('图片尺寸无效，请重新选择文件');

  let maxSide = Math.min(icon ? 512 : 1400, Math.max(image.naturalWidth, image.naturalHeight));
  const canvas = document.createElement('canvas');
  const context = canvas.getContext('2d');
  if (!context) throw new Error('浏览器无法处理图片，请更新浏览器后重试');
  while (true) {
    const ratio = Math.min(1, maxSide / Math.max(image.naturalWidth, image.naturalHeight));
    canvas.width = Math.max(1, Math.round(image.naturalWidth * ratio));
    canvas.height = Math.max(1, Math.round(image.naturalHeight * ratio));
    context.fillStyle = '#ffffff';
    context.fillRect(0, 0, canvas.width, canvas.height);
    context.drawImage(image, 0, 0, canvas.width, canvas.height);
    for (const quality of [0.85, 0.7, 0.5, 0.3]) {
      const output = canvas.toDataURL('image/jpeg', quality);
      if (output.startsWith('data:image/jpeg;base64,') && output.length < maxOutputLength) return output;
    }
    if (maxSide <= 256) break;
    maxSide = Math.max(256, Math.floor(maxSide * 0.75));
  }
  throw new Error('图片压缩后仍过大，请选择更小或更简单的图片');
}
