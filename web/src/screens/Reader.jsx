import { useEffect, useRef, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, RevBadge } from '../components/TopBar.jsx';
import { PdfViewer } from '../viewer/PdfViewer.jsx';
import { S } from '../strings.js';
import { app, getDB } from '../app.js';
import { useStore } from '../store.js';
import { fmtDate } from '../format.js';
import { CACHE_NAME, fileURL } from '../sync/sync.js';

const QUICK = [
  { n: 4, icon: 'croce', label: 'firstAid', cls: 'ps' },
  { n: 5, icon: 'fiamma', label: 'fire', cls: 'ai' },
  { n: 8, icon: 'casco', label: 'ppe', cls: 'dpi' },
];

export function Reader({ id }) {
  const s = useStore(app);
  const doc = s.docs.find((d) => d.id === id);
  const sec = doc && s.sections.find((x) => x.id === doc.section);
  const [buf, setBuf] = useState(null);
  const [sds, setSds] = useState(null);
  const [failed, setFailed] = useState(false);
  const [numPages, setNumPages] = useState(0);
  const [page, setPage] = useState(1);
  const viewer = useRef(null);
  const sha = doc && doc.sha256;

  // il PDF si legge solo dalla Cache Storage: niente rete
  useEffect(() => {
    let alive = true;
    setBuf(null);
    setFailed(false);
    if (!sha) return;
    (async () => {
      try {
        const cache = await caches.open(CACHE_NAME);
        const r = await cache.match(fileURL(sha));
        if (!r) throw new Error('assente');
        const b = await r.arrayBuffer();
        const rec = await getDB().getSds(sha);
        if (alive) {
          setSds(rec || null);
          setBuf(b);
        }
      } catch {
        if (alive) setFailed(true);
      }
    })();
    return () => {
      alive = false;
    };
  }, [sha]);

  // frecce della tastiera: cambio pagina; +/- zoom
  useEffect(() => {
    const k = (e) => {
      const v = viewer.current;
      if (!v || e.target.tagName === 'INPUT') return;
      if (e.key === 'ArrowRight' || e.key === 'PageDown') { v.next(); e.preventDefault(); }
      else if (e.key === 'ArrowLeft' || e.key === 'PageUp') { v.prev(); e.preventDefault(); }
      else if (e.key === '+' || e.key === '=') v.zoomIn();
      else if (e.key === '-') v.zoomOut();
      else if (e.key === 'Home') v.goTo(1);
      else if (e.key === 'End') v.goTo(numPages);
    };
    window.addEventListener('keydown', k);
    return () => window.removeEventListener('keydown', k);
  }, [numPages]);

  if (!doc) {
    return (
      <div class="screen">
        <TopBar left="back" title={S.appTitle} />
        <div class="body"><div class="vuoto"><div class="c"><Icon name="avviso" class="grande" /><p>{S.docUnavailable}</p></div></div></div>
      </div>
    );
  }

  const recognized = sds && sds.recognized;
  const map = (recognized && sds.sections) || {};
  const quick = QUICK.filter((q) => map[q.n]);
  // sezione corrente: la prima che inizia nella pagina visibile, altrimenti l'ultima iniziata prima
  let curSec = 0;
  for (let n = 1; n <= 16; n++) if (map[n] && map[n] < page) curSec = n;
  for (let n = 16; n >= 1; n--) if (map[n] === page) curSec = n;

  return (
    <div class="screen" data-testid="reader">
      <TopBar left="back" title={`${doc.title}${sec ? ` · ${sec.name}` : ''}`} badge={<RevBadge rev={doc.rev} />} />
      <div class="lettura">
        <aside class="pannello" data-testid="panel">
          {recognized ? (
            <>
              {quick.length > 0 && (
                <>
                  <h2 class="blocco-tit">{S.quickAccess}</h2>
                  <div class="rapido" data-testid="quick">
                    {quick.map((q) => (
                      <button key={q.n} class={`btn ${q.cls}`} onClick={() => viewer.current && viewer.current.goTo(map[q.n])}>
                        <Icon name={q.icon} />{S[q.label]}
                      </button>
                    ))}
                  </div>
                </>
              )}
              <h2 class="blocco-tit">{S.sectionsTitle}</h2>
              <div class="sez16">
                {S.sdsLabels.map((label, i) => {
                  const n = i + 1;
                  return (
                    <button key={n} disabled={!map[n]} class={curSec === n ? 'att' : ''} aria-label={`${n} ${label}`}
                      onClick={() => viewer.current && viewer.current.goTo(map[n])}>
                      <b>{n}</b><span>{label}</span>
                    </button>
                  );
                })}
              </div>
            </>
          ) : (
            <>
              <dl class="info-doc">
                <dt>{S.revision}</dt><dd>{doc.rev ? S.rev(doc.rev) : '–'}</dd>
                <dt>{S.updatedOn}</dt><dd>{fmtDate(doc.updated)}</dd>
                <dt>{S.pages}</dt><dd>{numPages || '–'}</dd>
              </dl>
              {numPages > 1 && (
                <>
                  <h2 class="blocco-tit">{S.pages}</h2>
                  <div class="pagine">
                    {Array.from({ length: numPages }, (_, i) => (
                      <button key={i} class={page === i + 1 ? 'att' : ''} onClick={() => viewer.current && viewer.current.goTo(i + 1)}>{S.page(i + 1)}</button>
                    ))}
                  </div>
                </>
              )}
            </>
          )}
          <div class="offline-ok"><Icon name="offline" />{S.offlineSince(fmtDate(doc.updated))}</div>
        </aside>
        {failed ? (
          <div class="vista"><div class="msg">{S.docUnavailable}</div></div>
        ) : buf ? (
          <PdfViewer data={buf} apiRef={viewer} loadingText={S.loadingDoc} onLoad={setNumPages} onPage={setPage} onError={() => setFailed(true)} />
        ) : (
          <div class="vista"><div class="msg">{S.loadingDoc}</div></div>
        )}
      </div>
      <div class="barra-inf">
        <div class="gr">
          <button class="btn" aria-label={S.zoomOut} title={S.zoomOut} onClick={() => viewer.current && viewer.current.zoomOut()}><Icon name="meno" /></button>
          <button class="btn" aria-label={S.zoomIn} title={S.zoomIn} onClick={() => viewer.current && viewer.current.zoomIn()}><Icon name="piu" /></button>
          <button class="btn" onClick={() => viewer.current && viewer.current.fit()}><Icon name="adatta" />{S.fit}</button>
        </div>
        <div class="gr">
          <button class="btn" aria-label={S.prevPage} disabled={page <= 1} onClick={() => viewer.current && viewer.current.prev()}><Icon name="indietro" /></button>
          <span class="pag" data-testid="page-of">{numPages ? S.pageOf(page, numPages) : ''}</span>
          <button class="btn" aria-label={S.nextPage} disabled={!numPages || page >= numPages} onClick={() => viewer.current && viewer.current.next()}><Icon name="avanti" /></button>
        </div>
      </div>
    </div>
  );
}
