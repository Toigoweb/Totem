// Manutenzione → Emergenza: avvio, annuncio vocale, chiusura e token per i sistemi esterni.
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { go, exitMaintenance, maintApi, setToast } from '../app.js';
import { fmtTime } from '../format.js';
import { checkEmergency } from '../kiosk/emergency.js';

const exit = () => {
  exitMaintenance();
  go('home');
};

export function EmergencyAdmin() {
  const [data, setData] = useState(null);
  const [err, setErr] = useState(null);
  const [tipo, setTipo] = useState('evacuazione');
  const [testo, setTesto] = useState('');
  const [confirm, setConfirm] = useState(null); // 'start' | 'stop'
  const [token, setToken] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = async () => {
    try {
      const r = await maintApi('/api/maint/emergency');
      if (r.ok) {
        setData(r.data);
        setTesto((t) => t || r.data.defaultEvacuation);
        setErr(null);
      }
    } catch {
      setErr(S.maintOffline);
    }
  };
  useEffect(() => {
    load();
  }, []);

  const call = async (path, body) => {
    setBusy(true);
    setErr(null);
    try {
      const r = await maintApi(path, { method: 'POST', body });
      if (!r.ok) {
        setErr(S.emErr[r.data.field] || S.emErr.io);
        return null;
      }
      await checkEmergency(); // anche questo dispositivo la mostra subito
      await load();
      return r.data;
    } catch {
      setErr(S.maintOffline);
      return null;
    } finally {
      setBusy(false);
      setConfirm(null);
    }
  };

  const pickTipo = (t) => {
    setTipo(t);
    if (t === 'evacuazione' && !testo.trim() && data) setTesto(data.defaultEvacuation);
    if (t === 'avviso' && data && testo === data.defaultEvacuation) setTesto('');
  };

  const newToken = async () => {
    const r = await maintApi('/api/maint/emergency/token', { method: 'POST' });
    if (r.ok) {
      setToken(r.data.token);
      load();
    }
  };
  const revokeToken = async () => {
    await maintApi('/api/maint/emergency/token', { method: 'DELETE' });
    setToken(null);
    load();
  };

  const st = data && data.state;
  const active = st && st.attiva;
  const max = (data && data.maxText) || 200;
  const origin = location.origin;
  return (
    <div class="screen" data-testid="emergency-admin">
      <TopBar left="back" onBack={() => go('maint')} title={S.emAdmin} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">
        {err && <div class="banner rosso" data-testid="emergency-err"><Icon name="errore" /><div>{err}</div></div>}
        {!data ? <p>{S.maintLoading}</p> : active ? (
          <div class="em-attiva" data-testid="emergency-active">
            <div class="banner rosso"><Icon name="avviso" /><div>
              <strong>{S.emActive}: {S.emTypes[st.tipo]}</strong>
              <p>{st.testo}</p>
              <p class="piccolo">{S.emStartedBy(st.da || '–', fmtTime(new Date(st.avviata)))}</p>
            </div></div>
            <div class="azioni">
              {data.tablets > 0 && (
                <button class="btn" disabled={busy} data-testid="emergency-announce" onClick={async () => {
                  const d = await call('/api/maint/emergency/announce');
                  if (d) setToast(S.emAnnounced(d.tablets));
                }}><Icon name="info" />{S.emAnnounce}</button>
              )}
              <button class="btn rosso" disabled={busy} data-testid="emergency-stop" onClick={() => setConfirm('stop')}><Icon name="x" />{S.emStop}</button>
            </div>
          </div>
        ) : (
          <>
            <p>{S.emAdminHelp}</p>
            <div class="form">
              <div class="campo largo">
                <span class="lab">{S.evType}</span>
                <div class="chips" role="group" aria-label={S.evType}>
                  {['evacuazione', 'avviso'].map((t) => (
                    <button key={t} type="button" class="chip" data-tipo={t} aria-pressed={tipo === t ? 'true' : 'false'} onClick={() => pickTipo(t)}>{S.emTypes[t]}</button>
                  ))}
                </div>
              </div>
              <div class="campo largo">
                <label for="em-testo">{S.emText}</label>
                <textarea id="em-testo" class="input" rows={3} maxLength={max} value={testo} onInput={(e) => setTesto(e.currentTarget.value)} />
                <span class="aiuto">{S.emTextHelp(max)} {S.emTablets(data.tablets)}</span>
              </div>
            </div>
            <div class="azioni" style={{ marginTop: '16px' }}>
              <button class="btn rosso" data-testid="emergency-start" disabled={busy || !testo.trim()} onClick={() => setConfirm('start')}>
                <Icon name="avviso" />{S.emStart}
              </button>
            </div>
          </>
        )}

        {data && (
          <>
            <h2 class="blocco-tit">{S.emApiTitle}</h2>
            <p class="piccolo">{S.emApiHelp}</p>
            <p><strong>{data.tokenActive ? S.emApiOn : S.emApiOff}</strong></p>
            {token && (
              <div class="banner" data-testid="emergency-token">
                <Icon name="lucchetto" />
                <div>
                  <p>{S.emApiShown}</p>
                  <code class="mono" style={{ wordBreak: 'break-all' }}>{token}</code>
                  <pre class="mono piccolo" style={{ whiteSpace: 'pre-wrap' }}>{`curl -X POST -H "Authorization: Bearer ${token}" ${origin}/api/emergency/start\ncurl -X POST -H "Authorization: Bearer ${token}" ${origin}/api/emergency/stop`}</pre>
                </div>
              </div>
            )}
            <div class="azioni">
              <button class="btn" data-testid="emergency-token-new" onClick={newToken}><Icon name="sync" />{S.emApiNew}</button>
              {data.tokenActive && <button class="btn" onClick={revokeToken}><Icon name="cestino" />{S.emApiRevoke}</button>}
            </div>
          </>
        )}
      </div>
      {confirm && (
        <div class="velo">
          <div class="dialogo" role="dialog" aria-modal="true" aria-labelledby="dlg-em">
            <h2 id="dlg-em">{confirm === 'start' ? S.emStartQ : S.emStopQ}</h2>
            <p>{confirm === 'start' ? S.emStartText : S.emStopText}</p>
            <div class="azioni">
              {confirm === 'start' ? (
                <button class="btn rosso" data-testid="confirm-emergency" disabled={busy} onClick={() => call('/api/maint/emergency/start', { tipo, testo })}>
                  <Icon name="avviso" />{S.emStart}
                </button>
              ) : (
                <button class="btn rosso" data-testid="confirm-emergency" disabled={busy} onClick={() => call('/api/maint/emergency/stop')}>
                  <Icon name="x" />{S.emStop}
                </button>
              )}
              <button class="btn" onClick={() => setConfirm(null)}>{S.cancel}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
