import { useEffect, useRef, useState } from 'preact/hooks';
import { Icon } from './Icon.jsx';
import { S } from '../strings.js';
import { app, go, back, pillState, getDB } from '../app.js';
import { useStore } from '../store.js';
import { fmtTime } from '../format.js';

const LONG_PRESS_MS = 2000;

// Logo aziendale: /brand/logo dal server (file logo.png o logo.svg in /data), conservato
// in IndexedDB per l'uso offline. Senza logo si mostra un marchio testuale.
// Si carica una sola volta per sessione, condiviso da tutte le barre.
let logoPromise;
function loadLogo() {
  if (!logoPromise) {
    logoPromise = (async () => {
      const db = getDB();
      let blob = db ? await db.getMeta('logo').catch(() => null) : null;
      try {
        const r = await fetch('/brand/logo', { cache: 'no-cache', credentials: 'same-origin' });
        if (r.ok) {
          blob = await r.blob();
          if (db) db.setMeta('logo', blob).catch(() => {});
        } else if (r.status === 404) {
          blob = null;
          if (db) db.setMeta('logo', null).catch(() => {});
        }
      } catch {
        // offline: si usa la copia locale
      }
      return blob ? URL.createObjectURL(blob) : null;
    })();
  }
  return logoPromise;
}

function useLogo() {
  const [url, setUrl] = useState(null);
  useEffect(() => {
    let alive = true;
    loadLogo().then((u) => alive && setUrl(u));
    return () => {
      alive = false;
    };
  }, []);
  return url;
}

/** Logo con pressione lunga di 2 secondi (dito o mouse) che apre il PIN, senza indicazioni. */
export function Logo({ onLongPress }) {
  const url = useLogo();
  const t = useRef(null);
  const start = useRef(null);
  const cancel = () => {
    clearTimeout(t.current);
    t.current = null;
  };
  const down = (e) => {
    if (!onLongPress || (e.pointerType === 'mouse' && e.button !== 0)) return;
    start.current = { x: e.clientX, y: e.clientY };
    cancel();
    t.current = setTimeout(() => {
      t.current = null;
      onLongPress();
    }, LONG_PRESS_MS);
  };
  const move = (e) => {
    if (t.current && start.current && Math.hypot(e.clientX - start.current.x, e.clientY - start.current.y) > 15) cancel();
  };
  useEffect(() => cancel, []);
  return (
    <div class="logo" data-testid="logo" onPointerDown={down} onPointerUp={cancel} onPointerLeave={cancel} onPointerCancel={cancel}
      onPointerMove={move} onContextMenu={(e) => e.preventDefault()}>
      {url ? <img src={url} alt="" draggable={false} /> : <span class="testo">SICUREZZA</span>}
    </div>
  );
}

export function StatusPill() {
  const s = useStore(app);
  const p = pillState(s);
  let icon = 'spunta';
  let text;
  let warn = false;
  switch (p.kind) {
    case 'syncing': icon = 'sync'; text = S.pillSyncing(p.done, p.total); break;
    case 'offline': icon = 'nuvolaOff'; text = S.pillOffline; break;
    case 'stale': icon = 'avviso'; text = S.pillStale(p.days); warn = true; break;
    case 'never': icon = 'avviso'; text = S.pillNever; warn = true; break;
    case 'device': icon = 'avviso'; text = S.pillDevice; warn = true; break;
    default: text = S.pillOk(fmtTime(p.at));
  }
  const onStatus = s.route.name === 'status';
  return (
    <button class={`pill ${warn ? 'avviso' : ''}`} data-testid="pill" data-kind={p.kind} onClick={() => !onStatus && go('status')}>
      <Icon name={icon} class={p.kind === 'syncing' ? 'gira' : ''} />
      <span>{text}</span>
    </button>
  );
}

/**
 * Barra superiore navy alta 72px.
 * left: 'logo' (con pressione lunga verso il PIN) | 'back' | 'logo-static'
 */
export function TopBar({ title, left = 'logo', onBack, right, badge }) {
  const paired = useStore(app, (s) => s.paired);
  return (
    <header class="topbar">
      <div class="sx">
        {left === 'back' ? (
          <button class="btn indietro" data-testid="back" onClick={onBack || back}><Icon name="indietro" />{S.back}</button>
        ) : (
          <Logo onLongPress={left === 'logo' && paired ? () => go('pin', { mode: 'login' }) : null} />
        )}
      </div>
      <h1><span class="t">{title}</span>{badge}</h1>
      <div class="dx">{right === undefined ? (paired ? <StatusPill /> : null) : right}</div>
    </header>
  );
}

export function MaintBand({ onExit }) {
  return (
    <div class="fascia-man" role="status">
      <Icon name="ingranaggio" />
      <span>{S.maintBand}</span>
      <button class="btn primario" data-testid="maint-exit" onClick={onExit}>{S.exit}</button>
    </div>
  );
}

export function Toggle({ on, onChange, label, help, id }) {
  return (
    <button type="button" class="toggle" role="switch" aria-checked={on ? 'true' : 'false'} id={id} onClick={() => onChange(!on)}>
      <span class={`sw ${on ? 'on' : ''}`} />
      <span class="t">{on ? S.yes : S.no}</span>
      {help && <span class="aiuto">{help}</span>}
      {label && <span class="sr">{label}</span>}
    </button>
  );
}

export function RevBadge({ rev }) {
  return rev ? <span class="badge">{S.rev(rev)}</span> : null;
}
