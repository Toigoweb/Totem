// Manutenzione → Dispositivi → Controllo: tablet con FreeKiosk (API REST), stato, comandi.
import { useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { Toggle } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, maintApi, setToast } from '../app.js';
import { fmtWhen } from '../format.js';

// stesse regole del server (internal/kiosk.NormalizeAddress), porta facoltativa
const ADDR = /^[A-Za-z0-9]([A-Za-z0-9.-]{0,251}[A-Za-z0-9])?(:\d{1,5})?$/;
export const validTabletAddress = (a) => ADDR.test(a.trim()) && (!a.includes(':') || (+a.split(':')[1] >= 1 && +a.split(':')[1] <= 65535));

function Row({ label, children, testid }) {
  return <div class="tab-riga" data-testid={testid}><span class="l">{label}</span><span class="v">{children}</span></div>;
}

/** Tablet con FreeKiosk collegato al dispositivo: stato, comandi, collegamento. */
export function TabletPanel({ dev, night, reload }) {
  const id = dev.id;
  const init = () => ({
    indirizzo: dev.kiosk ? dev.kiosk.indirizzo : '',
    chiave: '',
    schermoNotte: dev.kiosk ? dev.kiosk.schermoNotte : false,
    luminositaAuto: dev.kiosk ? dev.kiosk.luminositaAuto : true,
  });
  const [f, setF] = useState(init);
  const [test, setTest] = useState({ status: 'idle' });
  const [err, setErr] = useState(null);
  const [confirm, setConfirm] = useState(null);
  const [shot, setShot] = useState(null);
  const [busy, setBusy] = useState(false);

  const load = reload;
  const k = dev.kiosk;
  const set = (key) => (v) => setF((x) => ({ ...x, [key]: v }));
  const addrOk = validTabletAddress(f.indirizzo);
  const keyOk = f.chiave === '' ? !!(k && k.hasKey) : f.chiave.trim().length >= 8;

  const runTest = async () => {
    setTest({ status: 'testing' });
    try {
      const r = await maintApi('/api/maint/kiosk/test', { method: 'POST', body: { deviceId: id, indirizzo: f.indirizzo, chiave: f.chiave } });
      if (r.data.ok) setTest({ status: 'ok', text: S.tabTestOk(r.data.model, r.data.version, r.data.deviceOwner) });
      else setTest({ status: 'err', text: S.tabErr[r.data.code] || S.tabErr.unreachable });
    } catch {
      setTest({ status: 'err', text: S.maintOffline });
    }
  };
  const save = async (remove = false) => {
    setBusy(true);
    setErr(null);
    try {
      const body = remove ? { tipo: '' } : { tipo: 'freekiosk', ...f };
      const r = await maintApi(`/api/maint/devices/${id}/kiosk`, { method: 'PUT', body });
      if (r.ok) {
        setToast(S.saved2);
        setF((x) => ({ ...x, chiave: '' }));
        if (remove) setF(init());
        setTimeout(load, 1500);
      } else setErr(S.tabErr[r.data.field] || S.saveErr);
    } catch {
      setErr(S.maintOffline);
    }
    setBusy(false);
    setConfirm(null);
  };
  const action = async (name) => {
    setConfirm(null);
    setErr(null);
    try {
      const r = await maintApi(`/api/maint/devices/${id}/kiosk/${name}`, { method: 'POST' });
      if (r.data.ok) {
        setToast(S.tabActDone);
        setTimeout(load, 2000);
      } else setErr(S.tabErr[r.data.code] || S.tabErr.device);
    } catch {
      setErr(S.maintOffline);
    }
  };
  const screenshot = async () => {
    setErr(null);
    try {
      const m = app.get().maint;
      const r = await fetch(`/api/maint/devices/${id}/kiosk/screenshot`, { headers: { 'X-Maint-Token': m ? m.token : '' }, credentials: 'same-origin' });
      if (!r.ok) throw new Error('shot');
      if (shot) URL.revokeObjectURL(shot);
      setShot(URL.createObjectURL(await r.blob()));
    } catch {
      setErr(S.tabShotErr);
    }
  };

  const st = k && k.stato;
  return (
    <section class="ctl-blocco" data-testid="kiosk-device">
      <h2 class="blocco-tit">{S.tabButton}</h2>
      <div>
        <p class="piccolo">{S.tabHelp}</p>
        {err && <div class="banner rosso" data-testid="kiosk-err"><Icon name="errore" /><div>{err}</div></div>}

        {k && st && (
          <>
            <h3>{S.tabState}</h3>
            {st.issues.map((is) => (
              <div key={is.code} class={`banner ${is.error ? 'rosso' : ''}`} data-testid="kiosk-issue"><Icon name={is.error ? 'errore' : 'avviso'} /><div>{is.text}</div></div>
            ))}
            {!st.lastOk && !st.error ? <p>{S.tabNotYet}</p> : (
              <div class="tab-stato" data-testid="kiosk-state">
                {st.battery != null && <Row label={S.tabBattery} testid="kiosk-battery">{Math.round(st.battery)}% · {st.charging ? S.tabCharging : S.tabNotCharging}</Row>}
                {st.temperature != null && <Row label={S.tabTemp}>{Math.round(st.temperature)} °C</Row>}
                <Row label={S.tabScreen}>{st.nightOff ? S.tabNightOff : st.screenOn ? S.tabScreenOn : S.tabScreenOff}</Row>
                {st.wifi && <Row label={S.tabWifi}>{st.wifi}{st.rssi != null ? ` · ${Math.round(st.rssi)} dBm` : ''}</Row>}
                {st.freeMb != null && <Row label={S.tabFree}>{(st.freeMb / 1024).toFixed(1)} GB</Row>}
                {st.model && <Row label={S.tabModel}>{st.model}{st.android ? ` · Android ${st.android}` : ''}</Row>}
                {st.tiltDeg != null && <Row label={S.tabTilt}>{Math.round(st.tiltDeg)}°</Row>}
                <Row label={S.tabLast}>{st.lastOk ? fmtWhen(st.lastOk) : '–'}</Row>
              </div>
            )}

            <h3>{S.tabActions}</h3>
            <div class="griglia-btn">
              {['ricarica', 'riavvia-app', 'schermo-on', 'schermo-off', 'posizione'].map((a) => (
                <button key={a} class="btn" data-testid={`kiosk-act-${a}`} onClick={() => action(a)}><Icon name={a === 'posizione' ? 'mappa' : 'sync'} />{S.tabAct[a]}</button>
              ))}
              <button class="btn" data-testid="kiosk-act-riavvia-tablet" onClick={() => setConfirm('reboot')}><Icon name="sync" />{S.tabAct['riavvia-tablet']}</button>
              <button class="btn" data-testid="kiosk-shot" onClick={screenshot}><Icon name="info" />{S.tabShot}</button>
            </div>
            <p class="piccolo">{S.tabShotHelp}</p>
            {shot && <img src={shot} alt={S.tabShot} class="tab-shot" data-testid="kiosk-shot-img" />}
          </>
        )}

        <div class="form" style={{ marginTop: '16px' }}>
          <div class="campo">
            <label for="k-addr">{S.tabAddr}</label>
            <input id="k-addr" class={`input mono ${f.indirizzo && !addrOk ? 'err' : ''}`} value={f.indirizzo} placeholder="192.168.2.60:8080" autocomplete="off" spellcheck={false}
              onInput={(e) => set('indirizzo')(e.currentTarget.value)} />
            {f.indirizzo && !addrOk ? <span class="msg-err"><Icon name="errore" />{S.tabAddrErr}</span> : <span class="aiuto">{S.tabAddrHelp}</span>}
          </div>
          <div class="campo">
            <label for="k-key">{S.tabKey}</label>
            <input id="k-key" class="input" type="password" autocomplete="new-password" value={f.chiave} placeholder={k && k.hasKey ? S.tabKeyKeep : ''}
              onInput={(e) => set('chiave')(e.currentTarget.value)} />
            <span class="aiuto">{k && k.hasKey ? S.tabKeyKeep : S.tabKeyHelp}</span>
          </div>
          <div class="campo">
            <span class="lab">{S.tabNight}</span>
            <Toggle on={f.schermoNotte} onChange={set('schermoNotte')} help={S.tabNightHelp(night)} id="k-notte" />
          </div>
          <div class="campo">
            <span class="lab">{S.tabBright}</span>
            <Toggle on={f.luminositaAuto} onChange={set('luminositaAuto')} help={S.tabBrightHelp} id="k-luce" />
          </div>
          <div class="campo largo">
            <div class="azioni">
              <button class="btn" data-testid="kiosk-test" disabled={!addrOk || !keyOk || test.status === 'testing'} onClick={runTest}><Icon name="sync" />{S.tabTest}</button>
              {test.status === 'testing' && <span class="esito-prova" role="status"><span class="spinner" />{S.testing}</span>}
              {test.status === 'ok' && <span class="esito-prova ok" role="status" data-testid="kiosk-test-ok"><Icon name="spunta" />{test.text}</span>}
              {test.status === 'err' && <span class="esito-prova ko" role="status" data-testid="kiosk-test-err"><Icon name="errore" />{test.text}</span>}
            </div>
          </div>
        </div>
        <div class="azioni" style={{ marginTop: '16px' }}>
          <button class="btn primario" data-testid="kiosk-save" disabled={busy || !addrOk || !keyOk} onClick={() => save()}><Icon name="spunta" />{S.save}</button>
          {k && <button class="btn" onClick={() => setConfirm('remove')}><Icon name="cestino" />{S.tabRemove}</button>}
        </div>
      </div>
      {confirm && (
        <div class="velo">
          <div class="dialogo" role="dialog" aria-modal="true" aria-labelledby="dlg-k">
            <h2 id="dlg-k">{confirm === 'reboot' ? S.tabRebootQ : S.tabRemoveQ}</h2>
            <div class="azioni">
              <button class="btn rosso" data-testid="confirm-kiosk" onClick={() => (confirm === 'reboot' ? action('riavvia-tablet') : save(true))}>
                {confirm === 'reboot' ? S.tabAct['riavvia-tablet'] : S.tabRemove}
              </button>
              <button class="btn" onClick={() => setConfirm(null)}>{S.cancel}</button>
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
