import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, RevBadge } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go } from '../app.js';
import { useStore } from '../store.js';
import { fmtDate } from '../format.js';

function norm(t) {
  return t.toLowerCase().normalize('NFD').replace(/[̀-ͯ]/g, '');
}

export function Section({ id }) {
  const s = useStore(app);
  const [filter, setFilter] = useState('');
  const [sort, setSort] = useState('az');
  const [folder, setFolder] = useState('');
  const sec = s.sections.find((x) => x.id === id);
  useEffect(() => {
    if (!sec) go('home'); // sezione eliminata nel frattempo
  }, [sec]);
  if (!sec) return null;
  const all = s.docs.filter((d) => d.section === id);
  const folders = [...new Set(all.map((d) => d.folder).filter(Boolean))].sort((a, b) => a.localeCompare(b, 'it'));
  const f = norm(filter.trim());
  let docs = all.filter((d) => (!folder || d.folder === folder) && (!f || norm(`${d.title} ${d.path}`).includes(f)));
  docs = docs.sort(sort === 'az' ? (a, b) => a.title.localeCompare(b.title, 'it', { numeric: true }) : (a, b) => new Date(b.updated) - new Date(a.updated));

  return (
    <div class="screen" data-testid="section">
      <TopBar left="back" onBack={() => go('home')} title={sec.name} />
      <div class="body">
        <div class="strumenti">
          <label class="filtro">
            <Icon name="cerca" />
            <input type="search" value={filter} onInput={(e) => setFilter(e.currentTarget.value)} placeholder={S.filterPlaceholder}
              aria-label={S.filterPlaceholder} autocomplete="off" spellcheck={false} />
          </label>
          <div class="seg" role="group">
            <button aria-pressed={sort === 'az' ? 'true' : 'false'} onClick={() => setSort('az')}>{S.sortAZ}</button>
            <button aria-pressed={sort === 'recent' ? 'true' : 'false'} onClick={() => setSort('recent')}>{S.sortRecent}</button>
          </div>
        </div>
        {folders.length > 0 && (
          <div class="chips">
            <button class="chip" aria-pressed={!folder ? 'true' : 'false'} onClick={() => setFolder('')}>{S.allFolders}</button>
            {folders.map((x) => (
              <button key={x} class="chip" aria-pressed={folder === x ? 'true' : 'false'} onClick={() => setFolder(x)}><Icon name="cartella" />{x}</button>
            ))}
          </div>
        )}
        <div class="lista">
          {docs.map((d) => (
            <button key={d.id} class="riga" disabled={!d.available} data-testid="doc-row" onClick={() => go('reader', { id: d.id })}>
              <Icon name="pdf" class="pdf" />
              <span class="n">{d.title}</span>
              <RevBadge rev={d.rev} />
              <span class="d">{fmtDate(d.updated)}</span>
              {d.available ? (
                <span class="off"><Icon name="offline" />{S.offlineAvailable}</span>
              ) : (
                <span class="off"><Icon name="sync" />{S.downloading}</span>
              )}
            </button>
          ))}
        </div>
        {docs.length === 0 && <p class="piccolo">{all.length ? S.noFilterMatch : S.emptySection}</p>}
      </div>
    </div>
  );
}
