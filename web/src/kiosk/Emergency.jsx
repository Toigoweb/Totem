// Emergenza sul totem: a tutto schermo finché non viene terminata dal server. Si può
// ridurre a una fascia rossa per aprire un piano di emergenza; dopo un minuto senza
// tocchi torna a tutto schermo. In manutenzione resta solo la fascia.
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { S } from '../strings.js';
import { app, go } from '../app.js';
import { useStore } from '../store.js';
import { fmtTime } from '../format.js';

export const REOPEN_MS = 60000;

export function Emergency() {
  const s = useStore(app);
  const [, tick] = useState(0);
  const em = s.emergency;
  useEffect(() => {
    if (!em) return;
    const t = setInterval(() => {
      const cur = app.get();
      if (cur.emergency && !cur.emergencyOpen && !cur.maint && cur.route.name !== 'pin' && Date.now() - cur.lastInteraction >= REOPEN_MS) {
        app.set({ emergencyOpen: true });
      }
      tick((x) => x + 1);
    }, 5000);
    return () => clearInterval(t);
  }, [em && em.id]);
  if (!em) return null;

  const title = S.emTitle[em.type] || em.title;
  const since = em.since ? S.emSince(fmtTime(new Date(em.since))) : '';
  if (!s.emergencyOpen || s.maint || s.route.name === 'pin') {
    return (
      <div class="em-fascia" role="alert" data-testid="emergency-band">
        <Icon name="avviso" />
        <strong>{S.emBand}: {title}</strong>
        {!s.maint && s.route.name !== 'pin' && (
          <button class="btn" data-testid="emergency-show" onClick={() => app.set({ emergencyOpen: true })}>{S.emShow}</button>
        )}
        {s.maint && s.route.name !== 'emergencyAdmin' && (
          <button class="btn" onClick={() => go('emergencyAdmin')}>{S.emAdmin}</button>
        )}
      </div>
    );
  }
  const emergencySections = s.sections.filter((x) => x.emergency);
  const contacts = s.extras.contacts || [];
  const openSection = (id) => {
    app.set({ emergencyOpen: false, lastInteraction: Date.now() });
    go('section', { id });
  };
  return (
    <div class={`emergenza-schermo t-${em.type}`} role="alertdialog" aria-modal="true" aria-labelledby="em-t" data-testid="emergency-screen">
      <div class="c">
        <Icon name="avviso" class="grande" />
        <h1 id="em-t">{title}</h1>
        <p class="testo" data-testid="emergency-text">{em.text}</p>
        {since && <p class="da">{since}</p>}
        {contacts.length > 0 && (
          <ul class="numeri" aria-label={S.usefulNumbers}>
            {contacts.map((c, i) => <li key={i}><span class="num">{c.number}</span><span class="lab">{c.label}</span></li>)}
          </ul>
        )}
        {emergencySections.length > 0 && (
          <div class="azioni" role="group" aria-label={S.emOpenDocs}>
            {emergencySections.map((x) => (
              <button key={x.id} class="btn" onClick={() => openSection(x.id)}><Icon name={x.icon} />{x.name}</button>
            ))}
          </div>
        )}
        {s.paired && (
          <button class="btn fine" data-testid="emergency-end" onClick={() => { app.set({ emergencyOpen: false }); go('pin', { mode: 'login', next: 'emergencyAdmin' }); }}>
            <Icon name="lucchetto" />{S.emEnd}
          </button>
        )}
      </div>
    </div>
  );
}
