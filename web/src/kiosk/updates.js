// Aggiornamenti della PWA: il nuovo service worker resta in attesa e si attiva solo
// nella finestra notturna, dopo X minuti senza interazioni e con la Home aperta.
// Mai durante una lettura.
import { app } from '../app.js';

export function inWindow(win, d = new Date()) {
  const m = /^(\d\d):(\d\d)-(\d\d):(\d\d)$/.exec(win || '');
  if (!m) return false;
  const cur = d.getHours() * 60 + d.getMinutes();
  const a = +m[1] * 60 + +m[2];
  const b = +m[3] * 60 + +m[4];
  return a <= b ? cur >= a && cur < b : cur >= a || cur < b; // anche a cavallo della mezzanotte
}

export function shouldApply(s, now = new Date()) {
  const idleMin = s.settings.updateIdleMinutes || 15;
  return inWindow(s.settings.nightWindow || '02:00-05:00', now)
    && now.getTime() - s.lastInteraction >= idleMin * 60000
    && s.route.name === 'home' && !s.maint && !s.sync.running && !s.emergency;
}

export async function installUpdates() {
  if (!('serviceWorker' in navigator)) return;
  let reg;
  try {
    reg = await navigator.serviceWorker.register('/sw.js', { scope: '/' });
  } catch (e) {
    console.warn('service worker non registrato', e);
    return;
  }
  // alla prima installazione non c'è nulla da ricaricare
  const hadController = !!navigator.serviceWorker.controller;
  let reloading = false;
  navigator.serviceWorker.addEventListener('controllerchange', () => {
    if (reloading || !hadController) return;
    reloading = true;
    location.replace('/');
  });
  // controllo di nuove versioni ogni ora
  setInterval(() => reg.update().catch(() => {}), 60 * 60000);
  setInterval(() => {
    if (reg.waiting && shouldApply(app.get())) reg.waiting.postMessage({ type: 'SKIP_WAITING' });
  }, 60000);
}
