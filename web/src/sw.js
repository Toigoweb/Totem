// Service worker: precache della sola shell dell'app (Workbox). I PDF stanno in una cache
// separata gestita dalla sync e non passano da qui. Le API non vengono mai messe in cache.
import { precacheAndRoute, cleanupOutdatedCaches, createHandlerBoundToURL } from 'workbox-precaching';
import { NavigationRoute, registerRoute } from 'workbox-routing';
import { S } from './strings.js';

cleanupOutdatedCaches();
precacheAndRoute(self.__WB_MANIFEST);
registerRoute(new NavigationRoute(createHandlerBoundToURL('/index.html'), { denylist: [/^\/api\//, /^\/manifest\.json$/] }));

// Il nuovo service worker resta in attesa: l'app gli chiede di attivarsi solo di notte
// e dopo un periodo di inattività (mai durante la lettura).
// Alla prima installazione il service worker prende subito il controllo della pagina:
// così l'app funziona offline anche se il server cade prima di un ricaricamento.
// (Negli aggiornamenti si attiva solo con SKIP_WAITING e l'app si ricarica da sé.)
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

self.addEventListener('message', (e) => {
  if (e.data && e.data.type === 'SKIP_WAITING') self.skipWaiting();
});

// Interfono sul telefono: la chiamata arriva come notifica push anche con l'app chiusa.
// A fine squillo (risposto da un altro, nessuna risposta) la notifica diventa "persa":
// ogni push deve mostrare una notifica, altrimenti Chrome ne mostra una generica.
self.addEventListener('push', (e) => {
  let d = {};
  try {
    d = e.data ? e.data.json() : {};
  } catch {
    return;
  }
  const tag = `chiamata-${d.id}`;
  if (d.tipo === 'chiamata') {
    e.waitUntil(self.registration.showNotification(S.icPushTitle(d.verso || d.da), {
      body: `${S.icFrom}: ${d.da}`, tag, renotify: true, requireInteraction: true, vibrate: [600, 300, 600, 300, 600],
      data: d, actions: [{ action: 'rispondi', title: S.icAnswer }, { action: 'rifiuta', title: S.icReject }],
    }));
  } else if (d.tipo === 'fine') {
    e.waitUntil(self.registration.getNotifications({ tag }).then((list) => {
      const prev = list[0] && list[0].data;
      list.forEach((n) => n.close());
      if (prev) return self.registration.showNotification(S.icMissed, { body: `${S.icFrom}: ${prev.da}`, tag, silent: true });
      return undefined;
    }));
  }
});

self.addEventListener('notificationclick', (e) => {
  const d = e.notification.data || {};
  e.notification.close();
  if (d.tipo !== 'chiamata' || !d.id) return;
  if (e.action === 'rifiuta') {
    e.waitUntil(fetch(`/api/intercom/calls/${encodeURIComponent(d.id)}/reject`, {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}', credentials: 'same-origin',
    }).catch(() => {}));
    return;
  }
  const answer = e.action === 'rispondi';
  e.waitUntil(self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((list) => {
    for (const c of list) {
      c.postMessage({ type: 'chiamata', id: d.id, rispondi: answer });
      return c.focus();
    }
    return self.clients.openWindow(`/?chiamata=${encodeURIComponent(d.id)}${answer ? '&rispondi=1' : ''}`);
  }));
});
