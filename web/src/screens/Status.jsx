import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go, getManager, pillState, APP_VERSION, PLATFORM } from '../app.js';
import { useStore } from '../store.js';
import { api } from '../api.js';
import { fmtBytes, fmtTime, fmtWhen } from '../format.js';

// Consultabile senza PIN.
export function Status() {
  const s = useStore(app);
  const [server, setServer] = useState(undefined); // undefined = in caricamento, null = non raggiungibile
  const [est, setEst] = useState(null);

  const load = async () => {
    try {
      const r = await api('/api/status', { timeoutMs: 6000 });
      setServer(r.ok ? r.data : null);
      if (r.ok) app.set({ serverVersion: r.data.serverVersion });
    } catch {
      setServer(null);
    }
    if (navigator.storage && navigator.storage.estimate) navigator.storage.estimate().then(setEst, () => setEst(null));
  };
  useEffect(() => {
    load();
  }, [s.sync.running]);

  const p = pillState(s);
  const total = s.docs.filter((d) => d.available).length;
  const lastErr = s.sync.lastError || s.meta.lastError;
  const offline = server === null || (p.kind === 'offline');

  let main;
  if (s.sync.running) {
    const pr = s.sync.progress;
    main = { cls: '', icon: 'sync', text: pr && pr.total ? `${S.syncingMain} ${pr.done}/${pr.total}` : S.syncingMain };
  } else if (p.kind === 'device') main = { cls: 'vecchio', icon: 'avviso', text: S.deviceMain };
  else if (p.kind === 'never') main = { cls: 'vecchio', icon: 'avviso', text: S.neverSynced };
  else if (p.kind === 'stale') main = { cls: 'vecchio', icon: 'avviso', text: S.staleMain(p.days) };
  else if (offline) main = { cls: 'ko', icon: 'nuvolaOff', text: S.serverDown };
  else main = { cls: 'ok', icon: 'spunta', text: S.allGood };

  const secStatus = server ? server.sections : [];
  const banners = secStatus.filter((x) => x.scan && !x.scan.ok && x.scan.error);
  const localCount = (id) => s.docs.filter((d) => d.section === id && d.available).length;

  return (
    <div class="screen" data-testid="status">
      <TopBar left="back" title={S.statusTitle} />
      <div class="body">
        <div class={`stato-main ${main.cls}`} data-testid="status-main">
          <Icon name={main.icon} class={s.sync.running ? 'gira' : ''} />
          <div>
            <h2>{main.text}</h2>
            <dl>
              <div><dt>{S.lastSuccess}</dt><dd>{fmtWhen(s.meta.lastSuccess) || S.never}</dd></div>
              <div><dt>{S.nextAttempt}</dt><dd>{s.sync.running ? '–' : s.sync.nextAt ? fmtTime(s.sync.nextAt) : '–'}</dd></div>
              <div><dt>{S.totalDocs}</dt><dd>{total}</dd></div>
            </dl>
          </div>
        </div>

        {lastErr && lastErr.code === 'space' && (
          <div class="banner rosso" data-testid="space-banner"><Icon name="errore" /><div>{S.spaceBanner(fmtBytes(lastErr.needed), fmtBytes(lastErr.free))}</div></div>
        )}
        {lastErr && !['space', 'offline', 'device'].includes(lastErr.code) && !s.sync.running && (
          <div class="banner rosso"><Icon name="errore" /><div>{S.errorBanner(S.failReason[lastErr.code] || S.failReason.server)}</div></div>
        )}
        {banners.map((x) => (
          <div class="banner" key={x.id} data-testid="share-banner">
            <Icon name="avviso" />
            <div>{S.shareBanner(x.name, S.shareProblem[x.scan.error] || S.shareProblem.io, x.scan.since ? fmtTime(x.scan.since) : null)}</div>
          </div>
        ))}

        <div class="dett">
          <div>
            <h2 class="blocco-tit">{S.sectionsTitle}</h2>
            <table>
              <thead><tr><th>{S.colSection}</th><th>{S.colDocs}</th><th>{S.colScan}</th><th>{S.colDate}</th></tr></thead>
              <tbody>
                {s.sections.map((sec) => {
                  const st = secStatus.find((x) => x.id === sec.id);
                  const scan = st && st.scan;
                  let esito;
                  if (!server) esito = <span class="esito"><Icon name="info" />{S.scanUnknown}</span>;
                  else if (!scan || !scan.at || scan.at.startsWith('0001')) esito = <span class="esito"><Icon name="orologio" />{S.scanWaiting}</span>;
                  else if (scan.ok) esito = <span class="esito ok"><Icon name="spunta" />{S.scanOk}</span>;
                  else esito = <span class="esito ko"><Icon name="errore" />{S.scanFail(scan.error)}</span>;
                  return (
                    <tr key={sec.id}>
                      <td>{sec.name}</td>
                      <td>{localCount(sec.id)}</td>
                      <td>{esito}</td>
                      <td>{server && scan && scan.at && !scan.at.startsWith('0001') ? fmtWhen(scan.at) : '–'}</td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
          <div>
            <div>
              <h2 class="blocco-tit">{S.space}</h2>
              {est && est.quota ? (
                <>
                  <div class="spazio" role="img" aria-label={S.spaceText(fmtBytes(est.usage), fmtBytes(est.quota - est.usage))}>
                    <div style={{ width: `${Math.min(100, Math.max(1, (100 * est.usage) / est.quota))}%` }} />
                  </div>
                  <div class="piccolo">{S.spaceText(fmtBytes(est.usage), fmtBytes(est.quota - est.usage))}</div>
                </>
              ) : (
                <div class="piccolo">{S.spaceUnknown}</div>
              )}
              <div class="piccolo">{s.persist === 'granted' ? S.persistGranted : S.persistDenied}</div>
            </div>
            <div>
              <h2 class="blocco-tit">{S.lastSyncs}</h2>
              <ul class="log" data-testid="sync-log">
                {s.meta.syncLog.length === 0 && <li>{S.noSyncs}</li>}
                {s.meta.syncLog.slice(0, 10).map((e, i) => (
                  <li key={i}>{e.failed ? S.syncFailed(fmtWhen(e.at), S.failReason[e.failed] || S.failReason.server) : S.syncLine(fmtWhen(e.at), e.added, e.updated, e.removed, e.errors)}</li>
                ))}
              </ul>
            </div>
            <div class="piccolo">{S.versions(APP_VERSION, server ? server.serverVersion : null, PLATFORM)}</div>
          </div>
        </div>

        <div class="azioni">
          <button class="btn primario" data-testid="sync-now" disabled={s.sync.running || !s.paired}
            onClick={() => getManager() && getManager().syncNow({ manual: true })}>
            <Icon name="sync" class={s.sync.running ? 'gira' : ''} />{S.syncNow}
          </button>
          {p.kind === 'device' && <button class="btn" onClick={() => go('wizard')}><Icon name="ingranaggio" />{S.startSetup}</button>}
          {s.paired && !s.deviceError && <button class="btn" data-testid="maint-open" onClick={() => go('pin', { mode: 'login' })}><Icon name="lucchetto" />{S.maintenanceArea}</button>}
        </div>
      </div>
    </div>
  );
}
