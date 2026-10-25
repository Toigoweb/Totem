import { Icon } from '../components/Icon.jsx';
import { TopBar } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go, cycleScale, docsBySection, recentDocs, activeNotices, localDay, APP_VERSION } from '../app.js';
import { useStore } from '../store.js';
import { fmtDate } from '../format.js';
import { daysWithoutInjury } from '../heatmap.js';

function cols(n) {
  if (n <= 3) return Math.max(n, 1);
  if (n === 4) return 2;
  if (n <= 6) return 3;
  return 4;
}

/** Apre la sezione; se contiene un solo documento lo apre direttamente. */
export function openSection(sec, docs) {
  const avail = docs.filter((d) => d.available);
  if (docs.length === 1 && avail.length === 1) go('reader', { id: avail[0].id });
  else go('section', { id: sec.id });
}

function Footer() {
  return (
    <footer class="footer">
      <button class="btn" data-testid="scale" onClick={cycleScale}><Icon name="testo" />{S.textBigger}</button>
      <button class="btn" onClick={() => go('status')}><Icon name="info" />{S.status}</button>
      <span class="ver">v{APP_VERSION}</span>
    </footer>
  );
}

/** Avviso in Home: icona e testo, non solo colore. */
export function NoticeBanner({ n }) {
  const warn = n.level === 'attenzione';
  return (
    <div class={`banner ${warn ? '' : 'info'}`} role="status" data-testid="notice">
      <Icon name={warn ? 'avviso' : 'info'} />
      <div><strong>{warn ? S.noticeWarn : S.noticeInfo}:</strong> {n.text}</div>
    </div>
  );
}

export function Home() {
  const s = useStore(app);
  if (s.paired === false && !s.docs.length) {
    return (
      <div class="screen">
        <TopBar title={S.appTitle} right={null} />
        <div class="body">
          <div class="vuoto"><div class="c">
            <Icon name="ingranaggio" class="grande" />
            <h2>{S.notConfigured}</h2>
            <p>{S.notConfiguredHelp}</p>
            <button class="btn primario" data-testid="start-setup" onClick={() => go('wizard')}>{S.startSetup}</button>
          </div></div>
        </div>
        <footer class="footer"><span class="ver">v{APP_VERSION}</span></footer>
      </div>
    );
  }

  const bySec = docsBySection(s.docs);
  const emergency = s.sections.filter((x) => x.emergency);
  const recent = recentDocs(s.docs, s.settings.recentDays || 30);
  const secName = Object.fromEntries(s.sections.map((x) => [x.id, x.name]));
  const contacts = s.extras.contacts || [];
  const notices = activeNotices(s.extras.notices);
  const safety = s.extras.safety;
  const days = safety ? daysWithoutInjury(safety.lastInjury, safety.since, localDay()) : null;
  const title = s.extras.department ? `${S.appTitle} · ${s.extras.department.name}` : S.appTitle;

  return (
    <div class="screen" data-testid="home">
      <TopBar title={title} />
      <div class="body">
        <button class="cerca" data-testid="search-open" onClick={() => go('search')}>
          <Icon name="cerca" />{S.searchPlaceholder}
        </button>

        {notices.map((n) => <NoticeBanner key={n.id} n={n} />)}

        {(emergency.length > 0 || contacts.length > 0) && (
          <div class="emergenza" data-testid="emergency">
            <div class="tit"><Icon name="avviso" />{S.emergency}</div>
            {emergency.map((sec) => (
              <button key={sec.id} class="btn" onClick={() => openSection(sec, bySec.get(sec.id) || [])}>
                <Icon name={sec.icon} />{sec.name}
              </button>
            ))}
            {contacts.length > 0 && (
              <ul class="numeri" aria-label={S.usefulNumbers} data-testid="contacts">
                {contacts.map((c, i) => (
                  <li key={i}><span class="num">{c.number}</span><span class="lab">{c.label}</span></li>
                ))}
              </ul>
            )}
          </div>
        )}

        {s.sections.length === 0 ? (
          <div class="vuoto" style={{ minHeight: '240px' }}><div class="c">
            <Icon name="cartella" class="grande" />
            <h2>{s.sync.running ? S.firstSyncPending : S.noSections}</h2>
            {!s.sync.running && <p>{S.noSectionsHelp}</p>}
          </div></div>
        ) : (
          <div class="griglia" data-testid="grid" style={{ gridTemplateColumns: `repeat(${cols(s.sections.length)}, minmax(0, 1fr))` }}>
            {s.sections.map((sec) => {
              const n = (bySec.get(sec.id) || []).filter((d) => d.available).length;
              return (
                <button key={sec.id} class="tile" data-testid="tile" onClick={() => go('section', { id: sec.id })}>
                  <Icon name={sec.icon} />
                  <span class="nome">{sec.name}</span>
                  <span class="conta">{S.docCount(n)}</span>
                </button>
              );
            })}
          </div>
        )}

        {safety && safety.maps && safety.maps.length > 0 && (
          <button class="sic-home" data-testid="safety-open" onClick={() => go('safety')}>
            <Icon name="mappa" />
            <span class="t">{S.safetyTitle}<small>{S.safetyOpen}</small></span>
            {days !== null && <span class="g" data-testid="home-days"><strong>{days}</strong>{S.daysNoInjuryShort}</span>}
            <Icon name="avanti" />
          </button>
        )}

        {recent.length > 0 && (
          <>
            <h2 class="blocco-tit">{S.recent}</h2>
            <div class="recenti" data-testid="recent">
              {recent.map((d) => (
                <button key={d.id} class="scheda" onClick={() => go('reader', { id: d.id })}>
                  <span class={`badge ${d.badge === 'new' ? 'nuovo' : 'agg'}`}>{d.badge === 'new' ? S.badgeNew : S.badgeUpdated}</span>
                  <span class="n">{d.title}</span>
                  <span class="m">{secName[d.section] || ''} · {fmtDate(d.updated)}</span>
                </button>
              ))}
            </div>
          </>
        )}
      </div>
      <Footer />
    </div>
  );
}
