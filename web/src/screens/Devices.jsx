import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { go, exitMaintenance, maintApi, setToast } from '../app.js';
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
  const [reparti, setReparti] = useState([]);
  const load = async () => {
    try {
      const r = await maintApi('/api/maint/devices');
      if (r.ok) setList(r.data.devices);
      const x = await maintApi('/api/maint/extras');
      if (x.ok) setReparti(x.data.reparti);
    } catch {
      setErr(true);
    }
  };
  const setReparto = async (d, reparto) => {
    try {
      await maintApi(`/api/maint/devices/${d.id}`, { method: 'PUT', body: { reparto } });
      setToast(S.saved2);
    } catch {
      setErr(true);
    }
    load();
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
            {d.kiosk && (
              <div class="piccolo" data-testid="device-tablet">
                <Icon name={d.kiosk.stato.issues.some((i) => i.error) ? 'errore' : d.kiosk.stato.ok ? 'spunta' : 'avviso'} />{' '}
                FreeKiosk {d.kiosk.indirizzo}
                {d.kiosk.stato.battery != null && ` · ${S.tabBattery} ${Math.round(d.kiosk.stato.battery)}%${d.kiosk.stato.charging ? '' : ` (${S.tabNotCharging})`}`}
                {d.kiosk.stato.issues.length > 0 && ` · ${d.kiosk.stato.issues.map((i) => i.text).join(' · ')}`}
              </div>
            )}
            {d.agent && (
              <div class="piccolo" data-testid="device-pc">
                <Icon name={d.agent.stato.issues.some((i) => i.error) ? 'errore' : d.agent.stato.ok ? 'spunta' : 'avviso'} />{' '}
                {S.pcTitle}
                {d.agent.stato.issues.length > 0 && ` · ${d.agent.stato.issues.map((i) => i.text).join(' · ')}`}
              </div>
            )}
            {(d.view || d.battery || d.appIssues.length > 0) && (
              <div class="piccolo" data-testid="device-app">
                {d.view ? `${S.appView}: ${d.view}` : ''}
                {d.battery ? ` · ${S.appBattery} ${Math.round(d.battery.level)}%${d.battery.charging ? '' : ` (${S.tabNotCharging})`}` : ''}
                {d.appIssues.length > 0 && ` · ${d.appIssues.map((i) => i.text).join(' · ')}`}
              </div>
            )}
            {d.ruolo === 'totem' && reparti.length > 0 && (
              <label class="piccolo" style={{ display: 'flex', gap: '8px', alignItems: 'center', marginTop: '6px' }}>
                {S.department}
                <select class="input" style={{ width: 'auto', minHeight: '48px' }} value={d.reparto} data-testid="device-department"
                  onChange={(e) => setReparto(d, e.currentTarget.value)}>
                  <option value="">{S.noDepartment}</option>
                  {reparti.map((r) => <option key={r.id} value={r.id}>{r.nome}</option>)}
                </select>
              </label>
            )}
          </div>
          <div class="b">
            {d.ruolo !== 'telefono' && (
              <button class="btn" data-testid="open-tablet" onClick={() => go('kioskDevice', { id: d.id })}><Icon name="ingranaggio" />{S.ctlButton}</button>
            )}
            {d.ruolo === 'totem' && (
              <button class="btn" onClick={() => go('pairStation', { kind: 'replace', target: d.id, targetName: d.nome })}><Icon name="sync" />{S.replaceTotem}</button>
            )}
            {!d.corrente && <button class="btn" onClick={() => setConfirm(d)}><Icon name="cestino" />{S.revoke}</button>}
          </div>
        </div>
      ))}
      <div class="azioni">
        <button class="btn primario" data-testid="add-totem" onClick={() => go('pairStation', { kind: 'totem' })}><Icon name="piu" />{S.addTotem}</button>
        <button class="btn primario" onClick={() => go('pairStation', { kind: 'station' })}><Icon name="piu" />{S.pairStation}</button>
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

/**
 * Genera un codice monouso (10 minuti) per abbinare un altro dispositivo:
 * kind 'station' = postazione, 'totem' = nuovo totem (con reparto), 'replace' = sostituzione.
 */
export function PairStation({ kind = 'station', target, targetName }) {
  const [nome, setNome] = useState('');
  const [reparto, setReparto] = useState('');
  const [reparti, setReparti] = useState([]);
  const [code, setCode] = useState(null); // { code, until }
  const [err, setErr] = useState(null);
  const [, tick] = useState(0);
  useEffect(() => {
    if (kind === 'totem') maintApi('/api/maint/extras').then((r) => setReparti(r.data.reparti || []), () => {});
  }, [kind]);
  useEffect(() => {
    if (!code) return;
    const t = setInterval(() => tick((x) => x + 1), 1000);
    return () => clearInterval(t);
  }, [code]);
  const gen = async () => {
    setErr(null);
    try {
      const r = kind === 'station'
        ? await maintApi('/api/maint/stations/pair-code', { method: 'POST', body: { nome } })
        : await maintApi('/api/maint/totems/pair-code', { method: 'POST', body: kind === 'replace' ? { sostituisce: target } : { reparto } });
      if (r.ok) setCode({ code: r.data.code, until: Date.now() + r.data.expiresInSec * 1000 });
      else setErr(S.genericErr);
    } catch {
      setErr(S.maintOffline);
    }
  };
  const left = code ? (code.until - Date.now()) / 1000 : 0;
  const pretty = code ? `${code.code.slice(0, 4)}-${code.code.slice(4)}` : '';
  const title = kind === 'station' ? S.pairStationTitle : kind === 'totem' ? S.addTotemTitle : S.replaceTotemTitle(targetName || '');
  const help = kind === 'station' ? S.pairStationHelp : kind === 'totem' ? S.addTotemHelp : S.replaceTotemHelp;
  return (
    <div class="screen" data-testid="pair-station">
      <TopBar left="back" onBack={() => go('devices')} title={title} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">
        <div class="wiz">
          <p>{help}</p>
          {kind === 'station' && (
            <div class="campo">
              <label for="st-nome">{S.stationName}</label>
              <input id="st-nome" class="input" maxLength={40} value={nome} placeholder={S.stationNamePh} autocomplete="off"
                onInput={(e) => setNome(e.currentTarget.value)} />
            </div>
          )}
          {kind === 'totem' && reparti.length > 0 && (
            <div class="campo">
              <label for="tot-rep">{S.department}</label>
              <select id="tot-rep" class="input" value={reparto} onChange={(e) => setReparto(e.currentTarget.value)}>
                <option value="">{S.noDepartment}</option>
                {reparti.map((r) => <option key={r.id} value={r.id}>{r.nome}</option>)}
              </select>
            </div>
          )}
          <div class="azioni"><button class="btn primario" data-testid="gen-code" onClick={gen}><Icon name="lucchetto" />{S.generateCode}</button></div>
          {err && <div class="banner rosso"><Icon name="errore" /><div>{err}</div></div>}
          {code && (left > 0 ? (
            <div class="banner" data-testid="station-code">
              <Icon name="info" />
              <div style={{ display: 'grid', gap: '8px' }}>
                <span>{kind === 'station' ? S.stationCodeIs : S.totemCodeIs}</span>
                <strong class="codice" style={{ minHeight: 0, textAlign: 'left' }} data-code={code.code}>{pretty}</strong>
                <span>{S.stationCodeSteps(location.origin)}</span>
                <span>{S.stationCodeExpires(mmss(left))}</span>
              </div>
            </div>
          ) : (
            <div class="banner rosso"><Icon name="orologio" /><div>{S.stationCodeExpired}</div></div>
          ))}
        </div>
      </div>
    </div>
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
