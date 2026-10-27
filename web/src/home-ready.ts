// React mounting does not mean the first screen's image has decoded or its
// fonts have settled. Only wait for visible hero assets, never lazy lower pages.
export async function waitForHomePaint() {
  const images = Array.from(document.querySelectorAll<HTMLImageElement>('.home:not(.home-prerender) .home-hero img'));
  await Promise.all([
    document.fonts?.ready.catch(() => undefined),
    ...images.map(async image => {
      if (!image.complete) await new Promise<void>(resolve => {
        const done = () => { image.removeEventListener('load', done); image.removeEventListener('error', done); resolve(); };
        image.addEventListener('load', done); image.addEventListener('error', done);
        if (image.complete) done();
      });
      if (image.naturalWidth && image.decode) await image.decode().catch(() => undefined);
    }),
  ]);
  await new Promise<void>(resolve => requestAnimationFrame(() => requestAnimationFrame(() => resolve())));
}
