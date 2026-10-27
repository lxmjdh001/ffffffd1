import { afterEach, expect, it, vi } from 'vitest';
import { waitForHomePaint } from '../src/home-ready';
afterEach(()=>{vi.useRealTimers();vi.unstubAllGlobals();document.body.innerHTML='';});
it('waits for hero loading and decoding before allowing the overlay to close',async()=>{
 vi.useFakeTimers();vi.stubGlobal('requestAnimationFrame',(callback:()=>void)=>setTimeout(callback,16));
 document.body.innerHTML='<div class="home"><section class="home-hero"><img></section></div>';
 const image=document.querySelector('img')!;
 Object.defineProperty(image,'complete',{value:false});Object.defineProperty(image,'naturalWidth',{value:100});
 let decoded!:()=>void;image.decode=vi.fn(()=>new Promise<void>(resolve=>{decoded=resolve;}));
 let ready=false;const task=waitForHomePaint().then(()=>{ready=true;});
 await vi.advanceTimersByTimeAsync(13000);expect(ready).toBe(false);
 image.dispatchEvent(new Event('load'));await vi.advanceTimersByTimeAsync(0);expect(ready).toBe(false);
 decoded();await vi.advanceTimersByTimeAsync(0);expect(ready).toBe(false);
 await vi.advanceTimersByTimeAsync(32);await task;expect(ready).toBe(true);
});
it('allows a usable page after an image fails rather than leaving an endless loader',async()=>{
 vi.useFakeTimers();vi.stubGlobal('requestAnimationFrame',(callback:()=>void)=>setTimeout(callback,16));
 document.body.innerHTML='<div class="home"><section class="home-hero"><img></section></div>';
 const image=document.querySelector('img')!;Object.defineProperty(image,'complete',{value:false});
 const task=waitForHomePaint();image.dispatchEvent(new Event('error'));await vi.advanceTimersByTimeAsync(32);await task;
});
