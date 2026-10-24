import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import MiniSearch from 'minisearch';
import { Icon } from '../components/Icon.jsx';
import { RevBadge } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go } from '../app.js';
import { useStore } from '../store.js';
import { fmtDate } from '../format.js';

// Ricerca locale con MiniSearch su nome e metadati: nessuna chiamata di rete.
function normalize(t) {
  return t.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
}

export function buildIndex(docs, sections) {
  const secName = Object.fromEntries(sections.map((x) => [x.id, x.name]));
  const ms = new MiniSearch({
    fields: ['title', 'folder', 'file', 'sectionName', 'rev'],
    storeFields: ['id'],
    processTerm: (t) => normalize(t),
    searchOptions: { boost: { title: 3, file: 2 }, prefix: true, fuzzy: (term) => (term.length > 4 ? 0.2 : 0), combineWith: 'AND' },
  });
  ms.addAll(docs.map((d) => ({ id: d.id, title: d.title, folder: d.folder, file: d.path, sectionName: secName[d.section] || '', rev: d.rev })));
  return ms;
}

/** Evidenzia nel titolo le parti trovate (senza distinzione di accenti e maiuscole). */
export function highlight(title, terms) {
  if (!terms.length) return title;
  const norm = normalize(title);
  const marks = new Array(title.length).fill(false);
  for (const t of terms) {
    const nt = normalize(t);
    if (!nt) continue;
    let i = norm.indexOf(nt);
    while (i >= 0) {
      for (let k = i; k < i + nt.length; k++) marks[k] = true;
      i = norm.indexOf(nt, i + 1);
    }
  }
  const out = [];
  let buf = '';
  let cur = false;
  for (let i = 0; i < title.length; i++) {
    if (marks[i] !== cur) {
      if (buf) out.push(cur ? <mark>{buf}</mark> : buf);
      buf = '';
      cur = marks[i];
    }
    buf += title[i];
  }
  if (buf) out.push(cur ? <mark>{buf}</mark> : buf);
  return out;
}

export function Search() {
  const s = useStore(app);
  const [q, setQ] = useState('');
  const input = useRef(null);
  const index = useMemo(() => buildIndex(s.docs, s.sections), [s.docs, s.sections]);
  useEffect(() => {
    input.current && input.current.focus();
    const esc = (e) => e.key === 'Escape' && go('home');
    window.addEventListener('keydown', esc);
    return () => window.removeEventListener('keydown', esc);
  }, []);

  const query = q.trim();
  const results = query ? index.search(query) : [];
  const byId = new Map(s.docs.map((d) => [d.id, d]));
  const groups = s.sections
    .map((sec) => ({ sec, items: results.map((r) => ({ d: byId.get(r.id), terms: r.terms })).filter((x) => x.d && x.d.section === sec.id) }))
    .filter((g) => g.items.length);

  return (
    <div class="screen" data-testid="search">
      <div class="ricerca-top">
        <label class="cerca">
          <Icon name="cerca" />
          <input ref={input} type="search" value={q} onInput={(e) => setQ(e.currentTarget.value)} placeholder={S.searchPlaceholder}
            aria-label={S.searchPlaceholder} autocomplete="off" spellcheck={false} enterkeyhint="search" data-testid="search-input" />
        </label>
        <button class="btn primario" onClick={() => go('home')}><Icon name="x" />{S.close}</button>
      </div>
      <div class="body">
        {query && groups.length === 0 && (
          <div class="nessuno"><Icon name="cerca" /><strong>{S.noResults}</strong><span>{S.noResultsHelp}</span></div>
        )}
        {groups.map((g) => (
          <div class="gruppo" key={g.sec.id}>
            <h3><Icon name={g.sec.icon} />{g.sec.name} · {g.items.length}</h3>
            {g.items.map(({ d, terms }) => (
              <button class="res" key={d.id} disabled={!d.available} data-testid="result" onClick={() => go('reader', { id: d.id })}>
                <span>
                  <span class="n">{highlight(d.title, terms)}</span>
                  <br /><span class="s">{g.sec.name}{d.folder ? ` · ${d.folder}` : ''}{d.available ? '' : ` · ${S.downloading}`}</span>
                </span>
                <RevBadge rev={d.rev} />
                <span class="s">{fmtDate(d.updated)}</span>
              </button>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}
