// Modalità emergenza sul totem: il server la pubblica su /api/emergency, il totem la
// controlla ogni 10 secondi (5 durante l'emergenza) e la conserva in IndexedDB, così
// resta a schermo anche se poi la rete cade. La chiude solo il server (manutenzione,
// API esterna o comando sul server).
import { app, getDB } from '../app.js';
import { api } from '../api.js';

export const POLL_MS = 10000;
export const POLL_ACTIVE_MS = 5000;

/** Stato da salvare: solo i campi che servono, null se non attiva. */
export function normalize(d) {
  if (!d || !d.active) return null;
  return { id: d.id, type: d.type, title: d.title, text: d.text, since: d.since };
}

async function apply(next) {
  const cur = app.get().emergency;
  if ((cur && cur.id) === (next && next.id)) return;
  // un'emergenza nuova (o aggiornata) si apre a tutto schermo
  app.set({ emergency: next, emergencyOpen: !!next });
  try {
    await getDB().setMeta('emergency', next);
  } catch {
    // senza IndexedDB resta in memoria
  }
}

export async function checkEmergency() {
  try {
    const r = await api('/api/emergency', { timeoutMs: 6000 });
    if (r.ok) await apply(normalize(r.data));
  } catch {
    // offline: resta l'ultimo stato noto
  }
}

export async function installEmergency() {
  try {
    const saved = await getDB().getMeta('emergency');
    if (saved) app.set({ emergency: saved, emergencyOpen: true });
  } catch {
    // ignorato
  }
  const loop = async () => {
    if (app.get().paired) await checkEmergency();
    setTimeout(loop, app.get().emergency ? POLL_ACTIVE_MS : POLL_MS);
  };
  loop();
}
