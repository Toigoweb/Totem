// Salvaschermo del totem: dopo N minuti senza interazioni mostra a tutto schermo numeri
// utili e avvisi; nell'orario notturno (se attivato) uno schermo nero con l'ora.
// Il contenuto si sposta un poco ogni minuto per non segnare lo schermo.
// Si chiude con un tocco o un tasto; quel tocco non arriva ai comandi sottostanti.
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { S } from '../strings.js';
import { app, activeNotices, localDay } from '../app.js';
import { useStore } from '../store.js';
import { inWindow } from './updates.js';
import { fmtTime } from '../format.js';
import { daysWithoutInjury } from '../heatmap.js';

const BLOCKED = new Set(['wizard', 'pin', 'changePin']);

export function screensaverMode(s, now = new Date()) {
  const ss = s.extras && s.extras.screensaver;
  if (!ss || s.role !== 'totem' || s.maint || BLOCKED.has(s.route.name)) return null;
  const night = ss.night && inWindow(ss.nightHours, now);
  if (!ss.enabled && !night) return null;
  if (now.getTime() - s.lastInteraction < (ss.afterMinutes || 5) * 60000) return null;
  return night ? 'nero' : 'info';
}

export function Screensaver() {
  const s = useStore(app);
  const [shown, setShown] = useState(null);
  const [now, setNow] = useState(new Date());
  const [shift, setShift] = useState([0, 0]);

  useEffect(() => {
    const t = setInterval(() => setNow(new Date()), 5000);
    return () => clearInterval(t);
  }, []);
  const mode = screensaverMode(s, now);
  // si apre quando scatta la condizione; si chiude solo con il proprio tocco
  useEffect(() => {
    if (mode && !shown) setShown(mode);
    else if (mode && shown && mode !== shown) setShown(mode);
  }, [mode]);
  useEffect(() => {
    if (!shown) return;
    const t = setInterval(() => setShift([Math.round(Math.random() * 60 - 30), Math.round(Math.random() * 60 - 30)]), 60000);
    const k = () => close();
    window.addEventListener('keydown', k, true);
    return () => {
      clearInterval(t);
      window.removeEventListener('keydown', k, true);
    };
  }, [shown]);

  const close = (e) => {
    if (e) {
      e.preventDefault();
      e.stopPropagation();
    }
    app.set({ lastInteraction: Date.now() });
    setShown(null);
  };
  if (!shown) return null;
  const contacts = s.extras.contacts || [];
  const notices = activeNotices(s.extras.notices, now);
  const dep = s.extras.department;
  const sf = s.extras.safety;
  const days = sf ? daysWithoutInjury(sf.lastInjury, sf.since, localDay(now)) : null;
  return (
    <div class={`salvaschermo ${shown === 'nero' ? 'nero' : ''}`} data-testid="screensaver" data-mode={shown} onClick={close} role="button"
      aria-label={S.ssTouch}>
      <div class="c" style={{ transform: `translate(${shift[0]}px, ${shift[1]}px)` }}>
        <div class="ora">{fmtTime(now)}</div>
        {shown === 'info' && (
          <>
            <h1>{dep ? `${S.ssTitle} · ${dep.name}` : S.ssTitle}</h1>
            {contacts.length > 0 && (
              <div class="em">
                <div class="tit"><Icon name="avviso" />{S.emergency}</div>
                <ul class="numeri">
                  {contacts.map((c, i) => <li key={i}><span class="num">{c.number}</span><span class="lab">{c.label}</span></li>)}
                </ul>
              </div>
            )}
            {days !== null && <div class="giorni" data-testid="ss-days"><strong>{days}</strong> {S.daysNoInjuryShort}</div>}
            {notices.map((n) => (
              <div key={n.id} class="banner"><Icon name={n.level === 'attenzione' ? 'avviso' : 'info'} />
                <div><strong>{n.level === 'attenzione' ? S.noticeWarn : S.noticeInfo}:</strong> {n.text}</div></div>
            ))}
            <div class="tocca"><Icon name="info" />{S.ssTouch}</div>
          </>
        )}
      </div>
    </div>
  );
}
