// Errore JavaScript non gestito: l'app si ricarica sulla Home. Per evitare un ciclo di
// ricariche, dopo 3 ricariche in 5 minuti si aspetta un minuto prima della successiva.
const KEY = 'totem.reloads';

function recent() {
  try {
    const list = JSON.parse(sessionStorage.getItem(KEY) || '[]');
    return list.filter((t) => Date.now() - t < 5 * 60000);
  } catch {
    return [];
  }
}

export function reloadHome(reason) {
  const list = recent();
  const wait = list.length >= 3 ? 60000 : 0;
  try {
    sessionStorage.setItem(KEY, JSON.stringify([...list, Date.now()]));
  } catch {
    // nessuna memoria di sessione: si ricarica comunque
  }
  console.error('ricarica per errore non gestito:', reason);
  setTimeout(() => location.replace('/'), wait);
}

// errori innocui da non trattare come crash
function ignorable(err) {
  const name = err && err.name;
  const msg = String((err && err.message) || err || '');
  return name === 'RenderingCancelledException' || name === 'AbortError' || /ResizeObserver loop/.test(msg);
}

export function installErrorHandlers(win = window) {
  win.addEventListener('error', (e) => {
    if (!ignorable(e.error || e.message)) reloadHome(e.message);
  });
  win.addEventListener('unhandledrejection', (e) => {
    if (!ignorable(e.reason)) reloadHome(e.reason && e.reason.message);
  });
}
