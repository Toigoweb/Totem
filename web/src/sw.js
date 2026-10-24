// Service worker: precache della sola shell dell'app (Workbox). I PDF stanno in una cache
// separata gestita dalla sync e non passano da qui. Le API non vengono mai messe in cache.
import { precacheAndRoute, cleanupOutdatedCaches, createHandlerBoundToURL } from 'workbox-precaching';
import { NavigationRoute, registerRoute } from 'workbox-routing';

cleanupOutdatedCaches();
precacheAndRoute(self.__WB_MANIFEST);
registerRoute(new NavigationRoute(createHandlerBoundToURL('/index.html'), { denylist: [/^\/api\//, /^\/manifest\.json$/] }));

// Il nuovo service worker resta in attesa: l'app gli chiede di attivarsi solo di notte
// e dopo un periodo di inattività (mai durante la lettura).
self.addEventListener('message', (e) => {
  if (e.data && e.data.type === 'SKIP_WAITING') self.skipWaiting();
});
