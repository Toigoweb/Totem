// Manutenzione → Dispositivi → Controllo: un dispositivo visto da tre parti.
//   - App del totem: dal segnale periodico (versione, browser, batteria, cosa mostra) e
//     comandi all'app (ricarica, Home, sincronizza), consegnati entro 10 secondi;
//   - PC Windows: dall'agente (deploy/windows/totem-agente.ps1), comandi entro 30 secondi;
//   - Tablet FreeKiosk: dall'API del tablet (TabletPanel).
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand, Toggle } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go, exitMaintenance, maintApi, setToast } from '../app.js';
import { fmtWhen, fmtBytes } from '../format.js';
import { TabletPanel } from './KioskDevice.jsx';

const exit = () => {
  exitMaintenance();
  go('home');
};

function Row({ label, children, testid }) {
  return <div class="tab-riga" data-testid={testid}><span class="l">{label}</span><span class="v">{children}</span></div>;
}

function Issues({ list }) {
  return (list || []).map((is) => (
    <div key={is.code} class={`banner ${is.error ? 'rosso' : ''}`} data-testid="ctl-issue"><Icon name={is.error ? 'errore' : 'avviso'} /><div>{is.text}</div></div>
  ));
}

function AppPanel({ dev, setErr }) {
  const act = async (a) => {
    try {
      const r = await maintApi(`/api/maint/devices/${dev.id}/app/${a}`, { method: 'POST' });
      if (r.ok) setToast(S.appActDone);
      else setErr(S.genericErr);
    } catch {
      setErr(S.maintOffline);
    }
  };
  return (
    <section class="ctl-blocco" data-testid="ctl-app">
      <h2 class="blocco-tit">{S.appTitle2}</h2>
      <Issues list={dev.appIssues} />
      <div class="tab-stato">
        <Row label={S.lastContact}>{fmtWhen(dev.ultimoContatto) || S.never2}</Row>
        {dev.appVersion && <Row label={S.appVersionOf}>{dev.appVersion}</Row>}
        {dev.browser && <Row label={S.appBrowser}>{dev.browser}</Row>}
        {dev.view && <Row label={S.appView} testid="ctl-view">{dev.view}</Row>}
        {dev.battery && <Row label={S.appBattery} testid="ctl-app-battery">{Math.round(dev.battery.level)}% · {dev.battery.charging ? S.tabCharging : S.tabNotCharging}</Row>}
        {dev.documents != null && <Row label={S.docsOnDevice}>{dev.documents} · {S.freeOnDevice}: {dev.freeBytes ? fmtBytes(dev.freeBytes) : '–'}</Row>}
      </div>
      <div class="griglia-btn" style={{ marginTop: '12px' }}>
        {['ricarica', 'home', 'sincronizza'].map((a) => (
          <button key={a} class="btn" data-testid={`app-act-${a}`} onClick={() => act(a)}><Icon name={a === 'home' ? 'indietro' : 'sync'} />{S.appActs[a]}</button>
        ))}
      </div>
    </section>
  );
}

function PcPanel({ dev, night, reload, setErr }) {
  const ag = dev.agent;
  const [token, setToken] = useState(null);
  const [confirm, setConfirm] = useState(null);
  const [shot, setShot] = useState(null);
  const [waiting, setWaiting] = useState(false);
  const base = location.origin;

  const newToken = async () => {
    try {
      const r = await maintApi(`/api/maint/devices/${dev.id}/agent/token`, { method: 'POST' });
      if (r.ok) {
        setToken(r.data.token);
        reload();
      } else setErr(S.genericErr);
    } catch {
      setErr(S.maintOffline);
    }
  };
  const remove = async () => {
    setConfirm(null);
    await maintApi(`/api/maint/devices/${dev.id}/agent`, { method: 'DELETE' }).catch(() => {});
    setToken(null);
    reload();
  };
  const setNight = async (v) => {
    await maintApi(`/api/maint/devices/${dev.id}/agent`, { method: 'PUT', body: { schermoNotte: v } }).catch(() => {});
    reload();
  };
  const fetchShot = async (after) => {
    const m = app.get().maint;
    const r = await fetch(`/api/maint/devices/${dev.id}/agent/screenshot`, { headers: { 'X-Maint-Token': m ? m.token : '' }, credentials: 'same-origin' });
    if (!r.ok) return false;
    const at = Date.parse(r.headers.get('X-Scattato') || '');
    if (after && !(at >= after)) return false;
    if (shot) URL.revokeObjectURL(shot);
    setShot(URL.createObjectURL(await r.blob()));
    return true;
  };
  const act = async (a) => {
    setConfirm(null);
    try {
      const r = await maintApi(`/api/maint/devices/${dev.id}/agent/cmd/${a}`, { method: 'POST' });
      if (!r.ok) {
        setErr(S.genericErr);
        return;
      }
      if (a !== 'screenshot') {
        setToast(S.pcActDone);
        return;
      }
      // lo screenshot arriva al giro successivo dell'agente
      setWaiting(true);
      const asked = Date.now() - 2000;
      for (let i = 0; i < 25; i++) {
        await new Promise((res) => setTimeout(res, 2000));
        if (await fetchShot(asked).catch(() => false)) {
          setWaiting(false);
          return;
        }
      }
      setWaiting(false);
      setErr(S.pcShotErr);
    } catch {
      setErr(S.maintOffline);
    }
  };

  const st = ag && ag.stato;
  return (
    <section class="ctl-blocco" data-testid="ctl-pc">
      <h2 class="blocco-tit">{S.pcTitle}</h2>
      <p class="piccolo">{S.pcHelp}</p>
      {token && (
        <div class="banner" data-testid="pc-token">
          <Icon name="lucchetto" />
          <div>
            <p>{S.pcTokenShown}</p>
            <pre class="mono piccolo" style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{`powershell -ExecutionPolicy Bypass -File installa-kiosk.ps1 -Url ${base} -Utente totem -TokenAgente ${token}`}</pre>
          </div>
        </div>
      )}
      {!ag ? (
        <>
          <p>{S.pcNotConfigured}</p>
          <button class="btn primario" data-testid="pc-token-new" onClick={newToken}><Icon name="lucchetto" />{S.pcNewToken}</button>
        </>
      ) : (
        <>
          <Issues list={st.issues} />
          {!st.lastReport ? <p>{S.pcWaiting}</p> : (
            <div class="tab-stato" data-testid="pc-state">
              <Row label={S.pcWindows}>{st.windows || '–'}</Row>
              <Row label={S.pcChrome}>{st.chrome ? S.pcRunning : S.pcNotRunning}</Row>
              <Row label={S.pcPower} testid="pc-power">
                {st.mains == null ? S.pcUnknown : st.mains ? S.pcMains : S.pcOnBattery}
                {st.battery != null ? ` · ${Math.round(st.battery)}%` : ''}
              </Row>
              <Row label={S.tabScreen}>{st.screenOff ? S.tabScreenOff : S.tabScreenOn}</Row>
              <Row label={S.pcDisk}>{(st.diskFreeMb / 1024).toFixed(1)} GB</Row>
              <Row label={S.pcUptime}>{S.uptimeFmt(st.uptimeMin)}</Row>
              <Row label={S.pcUpdates}>{st.rebootPending ? S.pcUpdatesPending : S.pcUpdatesNone}</Row>
              <Row label={S.pcLast}>{fmtWhen(st.lastReport)}{st.version ? ` · ${S.pcAgent} ${st.version}` : ''}</Row>
              {st.lastResults.length > 0 && (
                <Row label={S.pcLastResults}>{st.lastResults.slice(0, 3).map((r) => (r.ok ? '✓' : `✗ ${r.errore || ''}`)).join(' · ')}</Row>
              )}
            </div>
          )}
          <div class="campo" style={{ margin: '12px 0' }}>
            <span class="lab">{S.pcNight}</span>
            <Toggle on={ag.schermoNotte} onChange={setNight} help={S.tabNightHelp(night)} id="pc-notte" />
          </div>
          <div class="griglia-btn">
            {['riavvia-chrome', 'schermo-on', 'schermo-off'].map((a) => (
              <button key={a} class="btn" data-testid={`pc-act-${a}`} onClick={() => act(a)}><Icon name="sync" />{S.pcActs[a]}</button>
            ))}
            <button class="btn" data-testid="pc-act-riavvia-pc" onClick={() => setConfirm('reboot')}><Icon name="sync" />{S.pcActs['riavvia-pc']}</button>
            <button class="btn" data-testid="pc-shot" disabled={waiting} onClick={() => act('screenshot')}><Icon name="info" />{S.pcActs.screenshot}</button>
          </div>
          {waiting && <p role="status"><span class="spinner" /> {S.pcShotWait}</p>}
          {shot && <img src={shot} alt={S.pcActs.screenshot} class="tab-shot" data-testid="pc-shot-img" />}
          <div class="azioni" style={{ marginTop: '12px' }}>
            <button class="btn" data-testid="pc-token-new" onClick={newToken}><Icon name="lucchetto" />{S.pcRenewToken}</button>
            <button class="btn" onClick={() => setConfirm('remove')}><Icon name="cestino" />{S.pcRemove}</button>
          </div>
        </>
      )}
      {confirm && (
        <div class="velo">
          <div class="dialogo" role="dialog" aria-modal="true" aria-labelledby="dlg-pc">
            <h2 id="dlg-pc">{confirm === 'reboot' ? S.pcRebootQ : S.pcRemoveQ}</h2>
            <div class="azioni">
              <button class="btn rosso" data-testid="confirm-pc" onClick={() => (confirm === 'reboot' ? act('riavvia-pc') : remove())}>
                {confirm === 'reboot' ? S.pcActs['riavvia-pc'] : S.pcRemove}
              </button>
              <button class="btn" onClick={() => setConfirm(null)}>{S.cancel}</button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}

export function DeviceControl({ id }) {
  const [dev, setDev] = useState(null);
  const [night, setNight] = useState('20:00-06:00');
  const [err, setErr] = useState(null);
  const [show, setShow] = useState({ pc: false, tablet: false });

  const load = async () => {
    try {
      const [r, x] = await Promise.all([maintApi('/api/maint/devices'), maintApi('/api/maint/extras')]);
      setDev((r.data.devices || []).find((y) => y.id === id) || null);
      if (x.ok && x.data.salvaschermo) setNight(x.data.salvaschermo.orarioNero || '20:00-06:00');
    } catch {
      setErr(S.maintOffline);
    }
  };
  useEffect(() => {
    load();
    const t = setInterval(load, 15000);
    return () => clearInterval(t);
  }, [id]);

  const showPc = dev && (dev.agent || dev.piattaforma === 'windows' || show.pc);
  const showTablet = dev && (dev.kiosk || dev.piattaforma === 'android' || show.tablet);
  return (
    <div class="screen" data-testid="device-control">
      <TopBar left="back" onBack={() => go('devices')} title={dev ? S.ctlTitle(dev.nome) : S.ctlButton} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">
        {err && <div class="banner rosso" data-testid="kiosk-err"><Icon name="errore" /><div>{err}</div></div>}
        {!dev ? <p>{S.maintLoading}</p> : (
          <>
            <AppPanel dev={dev} setErr={setErr} />
            {showPc && <PcPanel dev={dev} night={night} reload={load} setErr={setErr} />}
            {showTablet && <TabletPanel dev={dev} night={night} reload={load} />}
            {(!showPc || !showTablet) && (
              <div class="azioni" style={{ marginTop: '20px' }}>
                {!showPc && <button class="btn" data-testid="show-pc" onClick={() => setShow((x) => ({ ...x, pc: true }))}><Icon name="piu" />{S.pcTitle}</button>}
                {!showTablet && <button class="btn" data-testid="show-tablet" onClick={() => setShow((x) => ({ ...x, tablet: true }))}><Icon name="piu" />{S.tabButton}</button>}
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}
