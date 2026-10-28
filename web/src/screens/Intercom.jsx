// Interfono: rubrica (ruoli, totem e postazioni, telefoni) per il totem e per il telefono.
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, Toggle } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app } from '../app.js';
import { useStore } from '../store.js';
import { api } from '../api.js';
import { call, startIntercom, stopIntercom } from '../intercom/client.js';
import { pushSupport, enablePush, resubscribe } from '../intercom/push.js';

const REFRESH_MS = 5000;

/** Rubrica aggiornata ogni pochi secondi. dir: null finché non arriva, 'off' se l'interfono è spento. */
export function useDirectory() {
  const [dir, setDir] = useState(null);
  const [err, setErr] = useState(false);
  const busy = useStore(app, (s) => !!(s.call && s.call.phase !== 'ended'));
  const load = async () => {
    try {
      const r = await api('/api/intercom/directory', { timeoutMs: 8000 });
      if (r.status === 404) setDir('off');
      else if (r.ok) setDir(r.data);
      setErr(!r.ok && r.status !== 404);
    } catch {
      setErr(true);
    }
  };
  useEffect(() => {
    load();
    const t = setInterval(load, REFRESH_MS);
    return () => clearInterval(t);
  }, [busy]);
  return { dir, err, reload: load };
}

function Row({ name, sub, state, ok, onClick, testid }) {
  return (
    <button class={`ic-riga ${ok ? '' : 'spenta'}`} disabled={!ok} data-testid={testid} onClick={onClick}>
      <span class="n">{name}{sub && <small>{sub}</small>}</span>
      <span class={`st ${ok ? 'ok' : ''}`}><Icon name={ok ? 'spunta' : 'x'} />{state}</span>
      {ok && <Icon name="telefono" class="ch" />}
    </button>
  );
}

export function DirectoryList({ dir }) {
  const devState = (d) => (d.busy ? S.icBusy : d.reachable ? S.icReach : S.icUnreach);
  const desks = dir.devices.filter((d) => d.role !== 'telefono');
  const phones = dir.devices.filter((d) => d.role === 'telefono');
  if (!dir.groups.length && !dir.devices.length) return <p class="aiuto" data-testid="ic-empty">{S.icEmpty}</p>;
  return (
    <div class="ic-rubrica" data-testid="ic-directory">
      {dir.groups.length > 0 && <h2 class="blocco-tit">{S.icGroups}</h2>}
      {dir.groups.map((g) => (
        <Row key={g.id} testid="ic-group" name={g.name} state={S.icAvail(g.available)} ok={g.available > 0} onClick={() => call(`group:${g.id}`, g.name)} />
      ))}
      {desks.length > 0 && <h2 class="blocco-tit">{S.icDevices}</h2>}
      {desks.map((d) => (
        <Row key={d.id} testid="ic-device" name={d.name} sub={d.department} state={devState(d)} ok={d.reachable && !d.busy} onClick={() => call(`device:${d.id}`, d.name)} />
      ))}
      {phones.length > 0 && <h2 class="blocco-tit">{S.icPhones}</h2>}
      {phones.map((d) => (
        <Row key={d.id} testid="ic-phone" name={d.name} state={devState(d)} ok={d.reachable && !d.busy} onClick={() => call(`device:${d.id}`, d.name)} />
      ))}
    </div>
  );
}

/** Totem e postazioni: schermata aperta dal pulsante in Home. */
export function Intercom() {
  const { dir, err } = useDirectory();
  return (
    <div class="screen" data-testid="intercom">
      <TopBar left="back" title={S.icTitle} />
      <div class="body">
        <p class="ic-aiuto">{S.icHelp}</p>
        {err && <div class="banner rosso" role="alert"><Icon name="errore" /><div>{S.icLoadErr}</div></div>}
        {dir && dir !== 'off' && <DirectoryList dir={dir} />}
      </div>
    </div>
  );
}

/** Telefono: Home con disponibilità, notifiche e rubrica. */
export function PhoneHome() {
  const { dir, err, reload } = useDirectory();
  const [push, setPush] = useState(pushSupport());
  const on = dir && dir !== 'off';
  useEffect(() => {
    if (on) startIntercom();
    else if (dir === 'off') stopIntercom();
  }, [on, dir === 'off']);
  // permesso già dato ma il server non ha la sottoscrizione: la rinnova da solo
  useEffect(() => {
    if (on && push === 'on' && !dir.push && dir.vapidKey) resubscribe(dir.vapidKey).catch(() => {});
  }, [on && dir.push, push]);

  const setAvail = async (v) => {
    await api('/api/intercom/me', { method: 'PUT', body: { disponibile: v } }).catch(() => {});
    reload();
  };
  const notif = async () => {
    // con lo stesso tocco si chiede anche il microfono, così poi si risponde subito
    navigator.mediaDevices && navigator.mediaDevices.getUserMedia({ audio: true }).then((m) => m.getTracks().forEach((t) => t.stop())).catch(() => {});
    await enablePush(dir.vapidKey).catch(() => false);
    setPush(pushSupport());
    reload();
  };
  return (
    <div class="screen" data-testid="phone-home">
      <TopBar title={S.icPhoneTitle} right={null} />
      <div class="body">
        {err && <div class="banner rosso" role="alert"><Icon name="errore" /><div>{S.icLoadErr}</div></div>}
        {dir === 'off' && <div class="banner" data-testid="ic-phone-off"><Icon name="info" /><div>{S.icPhoneOff}</div></div>}
        {on && (
          <>
            <div class="campo ic-disp">
              <label for="ic-av">{S.icAvailable}</label>
              <Toggle id="ic-av" on={dir.available} onChange={setAvail} />
              <p class="aiuto">{S.icAvailableHelp}</p>
            </div>
            <div class="campo">
              <label>{S.icNotif}</label>
              {push === 'on' && dir.push && <p class="aiuto" data-testid="ic-notif-on"><Icon name="spunta" />{S.icNotifOn}</p>}
              {(push === 'off' || (push === 'on' && !dir.push)) && (
                <>
                  <p class="aiuto">{S.icNotifOff}</p>
                  {dir.vapidKey && <button class="btn primario" data-testid="ic-notif" onClick={notif}><Icon name="campana" />{S.icNotifEnable}</button>}
                </>
              )}
              {push === 'denied' && <p class="aiuto"><Icon name="avviso" />{S.icNotifDenied}</p>}
              {push === 'unsupported' && <p class="aiuto"><Icon name="avviso" />{S.icNotifNone}</p>}
            </div>
            <DirectoryList dir={dir} />
          </>
        )}
      </div>
    </div>
  );
}
