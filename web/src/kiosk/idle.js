// Inattività: dopo N minuti senza interazioni (10 in manutenzione) si torna alla Home,
// chiudendo ricerca, tastierini, finestre e modalità manutenzione. La procedura guidata
// non si interrompe.
import { app, go, exitMaintenance } from '../app.js';

const EVENTS = ['pointerdown', 'keydown', 'wheel', 'touchstart'];
export const MAINT_IDLE_MS = 10 * 60000;

export function lastInteraction() {
  return app.get().lastInteraction;
}

export function installIdle({ now = () => Date.now(), win = window } = {}) {
  const touch = () => app.set({ lastInteraction: now() });
  EVENTS.forEach((e) => win.addEventListener(e, touch, { passive: true, capture: true }));
  // anche lo scorrimento di un documento conta come lettura
  win.addEventListener('scroll', touch, { passive: true, capture: true });
  const timer = setInterval(() => {
    const s = app.get();
    // in manutenzione vale il timeout della fascia gialla (10 minuti), altrove quello del kiosk
    const idleMs = s.maint ? MAINT_IDLE_MS : (s.settings.idleMinutes || 3) * 60000;
    if (now() - s.lastInteraction < idleMs) return;
    if (s.route.name === 'wizard') return;
    const atHome = s.route.name === 'home' && !s.maint;
    if (!atHome) {
      if (s.maint) exitMaintenance();
      if (document.activeElement && document.activeElement.blur) document.activeElement.blur(); // chiude la tastiera
      go('home');
    }
  }, 5000);
  return () => {
    clearInterval(timer);
    EVENTS.forEach((e) => win.removeEventListener(e, touch, { capture: true }));
    win.removeEventListener('scroll', touch, { capture: true });
  };
}
