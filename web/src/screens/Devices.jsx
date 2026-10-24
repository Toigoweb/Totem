import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { go, exitMaintenance, maintApi } from '../app.js';
import { fmtBytes, fmtDate, fmtWhen, mmss } from '../format.js';

const exit = () => {
  exitMaintenance();
  go('home');
};

function Frame({ title, testid, children }) {
  return (
    <div class="screen" data-testid={testid}>
      <TopBar left="back" onBack={() => go('maint')} title={title} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">{children}</div>
    </div>
  );
}

const ROLE_ICON = { totem: 'scudo', postazione: 'ingranaggio', telefono: 'documento' };

/** Elenco dei dispositivi abbinati, con dati dell'ultimo contatto e revoca. */
export function Devices() {
  const [list, setList] = useState(null);
  const [err, setErr] = useState(false);
  const [confirm, setConfirm] = useState(null);
  const load = async () => {
    try {
      const r = await maintApi('/api/maint/devices');
      if (r.ok) setList(r.data.devices);
    } catch {
      setErr(true);
    }
  };
  useEffect(() => {
    load();
  }, []);
  const revoke = async (d) => {
    setConfirm(null);
    try {
      await maintApi(`/api/maint/devices/${d.id}`, { method: 'DELETE' });
    } catch {
      setErr(true);
    }
    load();
  };
  return (
    <Frame title={S.devicesTitle} testid="devices">
      {err && <div class="banner rosso"><Icon name="nuvolaOff" /><div>{S.maintOffline}</div></div>}
      {!list && !err && <p>{S.maintLoading}</p>}
      {list && list.map((d) => (
        <div class="sez-riga" key={d.id} data-testid="device-row">
          <Icon name={ROLE_ICON[d.ruolo] || 'documento'} />
          <div style={{ minWidth: 0 }}>
            <div class="n">
              {d.nome}
              <span class="badge">{S.roleNames[d.ruolo] || d.ruolo}</span>
              {d.corrente && <span class="badge nuovo">{S.thisDevice}</span>}
            </div>
            <div class="piccolo">
              {S.lastContact}: {fmtWhen(d.ultimoContatto) || S.never2} · {d.piattaforma}
              {d.appVersion ? ` · ${S.appVersionOf} ${d.appVersion}` : ''}
            </div>
            {d.documents != null && (
              <div class="piccolo">
                {S.docsOnDevice}: {d.documents} · {S.lastSyncOf}: {fmtWhen(d.lastSync) || S.never2} · {S.freeOnDevice}: {d.freeBytes ? fmtBytes(d.freeBytes) : '–'}
              </div>
            )}
          </div>
          <div class="b">
            {d.ruolo === 'totem' ? <span class="piccolo" style={{ maxWidth: '16em' }}>{S.totemNote}</span>
              : !d.corrente && <button class="btn" onClick={() => setConfirm(d)}><Icon name="cestino" />{S.revoke}</button>}
          </div>
        </div>
      ))}
      <div class="azioni">
        <button class="btn primario" onClick={() => go('pairStation')}><Icon name="piu" />{S.pairStation}</button>
      </div>
      {confirm && (
        <div class="velo">
          <div class="dialogo" role="dialog" aria-modal="true" aria-labelledby="dlg-r">
            <h2 id="dlg-r">{S.revokeQ(confirm.nome)}</h2>
            <p>{S.revokeText}</p>
            <div class="azioni">
              <button class="btn rosso" data-testid="confirm-revoke" onClick={() => revoke(confirm)}><Icon name="cestino" />{S.revoke}</button>
              <button class="btn" onClick={() => setConfirm(null)}>{S.cancel}</button>
            </div>
          </div>
        </div>
      )}
    </Frame>
  );
}

/** Genera il codice monouso (10 minuti) per abbinare un altro PC come postazione. */
export function PairStation() {
  const [nome, setNome] = useState('');
  const [code, setCode] = useState(null); // { code, until }
  const [err, setErr] = useState(null);
  const [, tick] = useState(0);
  useEffect(() => {
    if (!code) return;
    const t = setInterval(() => tick((x) => x + 1), 1000);
    return () => clearInterval(t);
  }, [code]);
  const gen = async () => {
    setErr(null);
    try {
      const r = await maintApi('/api/maint/stations/pair-code', { method: 'POST', body: { nome } });
      if (r.ok) setCode({ code: r.data.code, until: Date.now() + r.data.expiresInSec * 1000 });
      else setErr(S.genericErr);
    } catch {
      setErr(S.maintOffline);
    }
  };
  const left = code ? (code.until - Date.now()) / 1000 : 0;
  const pretty = code ? `${code.code.slice(0, 4)}-${code.code.slice(4)}` : '';
  return (
    <Frame title={S.pairStationTitle} testid="pair-station">
      <div class="wiz">
        <p>{S.pairStationHelp}</p>
        <div class="campo">
          <label for="st-nome">{S.stationName}</label>
          <input id="st-nome" class="input" maxLength={40} value={nome} placeholder={S.stationNamePh} autocomplete="off"
            onInput={(e) => setNome(e.currentTarget.value)} />
        </div>
        <div class="azioni"><button class="btn primario" data-testid="gen-code" onClick={gen}><Icon name="lucchetto" />{S.generateCode}</button></div>
        {err && <div class="banner rosso"><Icon name="errore" /><div>{err}</div></div>}
        {code && (left > 0 ? (
          <div class="banner" data-testid="station-code">
            <Icon name="info" />
            <div style={{ display: 'grid', gap: '8px' }}>
              <span>{S.stationCodeIs}</span>
              <strong class="codice" style={{ minHeight: 0, textAlign: 'left' }} data-code={code.code}>{pretty}</strong>
              <span>{S.stationCodeSteps(location.origin)}</span>
              <span>{S.stationCodeExpires(mmss(left))}</span>
            </div>
          </div>
        ) : (
          <div class="banner rosso"><Icon name="orologio" /><div>{S.stationCodeExpired}</div></div>
        ))}
      </div>
    </Frame>
  );
}

/** PDF scartati dal server: cosa correggere nelle cartelle di rete. */
export function Problems() {
  const [list, setList] = useState(null);
  const [err, setErr] = useState(false);
  useEffect(() => {
    maintApi('/api/maint/problems').then((r) => setList(r.data.problems || []), () => setErr(true));
  }, []);
  return (
    <Frame title={S.problemsTitle} testid="problems">
      <p class="piccolo">{S.problemsHelp}</p>
      {err && <div class="banner rosso"><Icon name="nuvolaOff" /><div>{S.maintOffline}</div></div>}
      {list && list.length === 0 && (
        <div class="nessuno"><Icon name="spunta" /><strong>{S.problemsNone}</strong></div>
      )}
      {list && list.length > 0 && (
        <table data-testid="problems-table">
          <thead><tr><th>{S.colSection}</th><th>{S.colFile}</th><th>{S.colReason}</th><th>{S.colDate}</th></tr></thead>
          <tbody>
            {list.map((p) => (
              <tr key={p.section + p.path}>
                <td>{p.sectionName}</td>
                <td style={{ wordBreak: 'break-all' }}>{p.path}</td>
                <td>
                  <span class="esito ko"><Icon name="errore" />{S.problemReason[p.code] || p.code}</span>
                  <div class="piccolo">{p.hasPrevious ? S.problemPrev : S.problemNoPrev}</div>
                </td>
                <td>{fmtDate(p.at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </Frame>
  );
}
