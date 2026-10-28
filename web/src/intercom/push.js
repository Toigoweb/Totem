// Notifiche push del telefono: la chiamata squilla anche con l'app chiusa.
import { api } from '../api.js';

export function b64ToBytes(s) {
  const pad = '='.repeat((4 - (s.length % 4)) % 4);
  const bin = atob((s + pad).replace(/-/g, '+').replace(/_/g, '/'));
  return Uint8Array.from(bin, (c) => c.charCodeAt(0));
}

/** 'on' | 'off' | 'denied' | 'unsupported' */
export function pushSupport() {
  if (typeof Notification === 'undefined' || !('serviceWorker' in navigator) || typeof PushManager === 'undefined') return 'unsupported';
  if (Notification.permission === 'denied') return 'denied';
  return Notification.permission === 'granted' ? 'on' : 'off';
}

/** Chiede il permesso (serve un tocco), si iscrive e manda la sottoscrizione al server. */
export async function enablePush(vapidKey) {
  if (pushSupport() === 'unsupported' || !vapidKey) return false;
  const perm = await Notification.requestPermission();
  if (perm !== 'granted') return false;
  return resubscribe(vapidKey);
}

/** Con il permesso già dato: rinnova la sottoscrizione (il servizio può cambiarla). */
export async function resubscribe(vapidKey) {
  const reg = await navigator.serviceWorker.ready;
  let sub = await reg.pushManager.getSubscription();
  if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: b64ToBytes(vapidKey) });
  const r = await api('/api/intercom/me', { method: 'PUT', body: { push: sub.toJSON() } });
  return r.ok;
}
