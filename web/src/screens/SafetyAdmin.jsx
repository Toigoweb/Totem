// Manutenzione della mappa incidenti: planimetrie, eventi, data di inizio, esportazione.
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go, exitMaintenance, maintApi, setToast, getManager, localDay } from '../app.js';
import { Chips } from './Extras.jsx';
import { MapView, EvMark } from './SafetyMap.jsx';
import { EVENT_TYPES } from '../heatmap.js';

const exit = () => {
  exitMaintenance();
  go('home');
};

function syncSoon() {
  const m = getManager();
  if (m) setTimeout(() => m.syncNow(), 1500);
}

/** Richiesta con corpo binario (immagine) e token di manutenzione. */
async function sendRaw(path, file) {
  const m = app.get().maint;
  const r = await fetch(path, { method: 'POST', body: file, credentials: 'same-origin',
    headers: { 'Content-Type': file.type || 'application/octet-stream', 'X-Maint-Token': m ? m.token : '' } });
  let data = {};
  try {
    data = await r.json();
  } catch {
    data = {};
  }
  return { ok: r.ok, status: r.status, data };
}

function EventDialog({ ev, onClose, onSaved }) {
  const isNew = !ev.id;
  const [f, setF] = useState({ data: ev.data || localDay(), tipo: ev.tipo || '', nota: ev.nota || '' });
  const [err, setErr] = useState(null);
  const [busy, setBusy] = useState(false);
  const [confirm, setConfirm] = useState(false);
  const save = async () => {
    if (!f.tipo) {
      setErr(S.evErr.tipo);
      return;
    }
    setBusy(true);
    setErr(null);
    try {
      const body = { mappa: ev.mappa, x: ev.x, y: ev.y, data: f.data, tipo: f.tipo, nota: f.nota };
      const r = isNew ? await maintApi('/api/maint/safety/events', { method: 'POST', body })
        : await maintApi(`/api/maint/safety/events/${ev.id}`, { method: 'PUT', body });
      if (r.ok) {
        onSaved();
        return;
      }
      setErr(S.evErr[r.data.field] || S.evErr.io);
    } catch {
      setErr(S.maintOffline);
    }
    setBusy(false);
  };
  const del = async () => {
    try {
      const r = await maintApi(`/api/maint/safety/events/${ev.id}`, { method: 'DELETE' });
      if (r.ok) onSaved();
      else setErr(S.evErr.io);
    } catch {
      setErr(S.maintOffline);
    }
  };
  return (
    <div class="velo">
      <div class="dialogo info" role="dialog" aria-modal="true" aria-labelledby="dlg-ev" data-testid="event-dialog">
        <h2 id="dlg-ev">{isNew ? S.evNew : S.evEdit}</h2>
        <div class="form">
          <div class="campo">
            <label for="ev-data">{S.evDate}</label>
            <input id="ev-data" type="date" class="input" value={f.data} max={localDay()} onInput={(e) => setF({ ...f, data: e.currentTarget.value })} />
          </div>
          <div class="campo largo">
            <span class="lab">{S.evType}</span>
            <div class="chips" role="group" aria-label={S.evType}>
              {EVENT_TYPES.map((t) => (
                <button key={t} type="button" class="chip" data-tipo={t} aria-pressed={f.tipo === t ? 'true' : 'false'} onClick={() => setF({ ...f, tipo: t })}>
                  <EvMark type={t} />{S.evType1[t]}
                </button>
              ))}
            </div>
          </div>
          <div class="campo largo">
            <label for="ev-nota">{S.evNote}</label>
            <textarea id="ev-nota" class="input" rows={2} maxLength={200} value={f.nota} onInput={(e) => setF({ ...f, nota: e.currentTarget.value })} />
            <span class="aiuto">{S.evNoteHelp}</span>
          </div>
        </div>
        {err && <div class="banner rosso" data-testid="event-err"><Icon name="errore" /><div>{err}</div></div>}
        {confirm ? (
          <div class="azioni">
            <span>{S.evDelQ}</span>
            <button class="btn rosso" data-testid="confirm-delete" onClick={del}><Icon name="cestino" />{S.del}</button>
            <button class="btn" onClick={() => setConfirm(false)}>{S.cancel}</button>
          </div>
        ) : (
          <div class="azioni">
            <button class="btn primario" data-testid="event-save" disabled={busy} onClick={save}><Icon name="spunta" />{S.save}</button>
            {!isNew && <button class="btn" data-testid="event-delete" onClick={() => setConfirm(true)}><Icon name="cestino" />{S.del}</button>}
            <button class="btn" onClick={onClose}>{S.cancel}</button>
          </div>
        )}
      </div>
    </div>
  );
}

export function SafetyAdmin() {
  const [data, setData] = useState(null);
  const [depts, setDepts] = useState([]);
  const [err, setErr] = useState(null);
  const [sel, setSel] = useState(null);
  const [name, setName] = useState('');
  const [mapErr, setMapErr] = useState(null);
  const [busy, setBusy] = useState(false);
  const [edit, setEdit] = useState(null); // { id, nome, reparti }
  const [confirmMap, setConfirmMap] = useState(null);
  const [ev, setEv] = useState(null);
  const [all, setAll] = useState(false);
  const [start, setStart] = useState('');

  const load = async () => {
    try {
      const [a, b] = await Promise.all([maintApi('/api/maint/safety'), maintApi('/api/maint/extras')]);
      if (!a.ok) {
        setErr(S.genericErr);
        return;
      }
      setData(a.data);
      setStart(a.data.inizio || '');
      setDepts(b.ok ? b.data.reparti : []);
      setSel((cur) => (a.data.mappe.some((m) => m.id === cur) ? cur : (a.data.mappe[0] || {}).id || null));
      setErr(null);
    } catch {
      setErr(S.maintOffline);
    }
  };
  useEffect(() => {
    load();
  }, []);

  const changed = () => {
    syncSoon();
    load();
  };

  const addMap = async (file) => {
    if (!file) return;
    if (!name.trim()) {
      setMapErr(S.mapErr.nome);
      return;
    }
    setBusy(true);
    setMapErr(null);
    try {
      const r = await sendRaw(`/api/maint/safety/maps?nome=${encodeURIComponent(name.trim())}`, file);
      if (r.ok) {
        setName('');
        setSel(r.data.id);
        setToast(S.saved2);
        changed();
      } else if (r.status === 401) {
        app.set({ maint: null });
        go('home');
      } else setMapErr(r.status === 413 ? S.mapErr.too_large : S.mapErr[r.data.field] || S.mapErr.io);
    } catch {
      setMapErr(S.maintOffline);
    }
    setBusy(false);
  };

  const saveMap = async () => {
    try {
      const r = await maintApi(`/api/maint/safety/maps/${edit.id}`, { method: 'PUT', body: { nome: edit.nome, reparti: edit.reparti } });
      if (r.ok) {
        setEdit(null);
        setToast(S.saved2);
        changed();
      } else setMapErr(S.mapErr[r.data.field] || S.mapErr.io);
    } catch {
      setMapErr(S.maintOffline);
    }
  };

  const delMap = async (m) => {
    setConfirmMap(null);
    try {
      await maintApi(`/api/maint/safety/maps/${m.id}`, { method: 'DELETE' });
      changed();
    } catch {
      setErr(S.maintOffline);
    }
  };

  const saveStart = async () => {
    try {
      const r = await maintApi('/api/maint/safety/settings', { method: 'PUT', body: { inizio: start } });
      if (r.ok) {
        setToast(S.saved2);
        changed();
      } else setErr(S.evErr.data);
    } catch {
      setErr(S.maintOffline);
    }
  };

  const exportCsv = async () => {
    const m = app.get().maint;
    try {
      const r = await fetch('/api/maint/safety/export', { headers: { 'X-Maint-Token': m ? m.token : '' }, credentials: 'same-origin' });
      if (!r.ok) throw new Error('export');
      const u = URL.createObjectURL(await r.blob());
      const a = document.createElement('a');
      a.href = u;
      a.download = `eventi-sicurezza-${localDay()}.csv`;
      a.click();
      setTimeout(() => URL.revokeObjectURL(u), 10000);
    } catch {
      setErr(S.genericErr);
    }
  };

  const map = data && data.mappe.find((m) => m.id === sel);
  const evs = data && map ? data.eventi.filter((e) => e.mappa === map.id) : [];
  const deptName = Object.fromEntries(depts.map((d) => [d.id, d.nome]));
  const markers = evs.map((e) => ({ ...e, date: e.data, type: e.tipo }));

  return (
    <div class="screen" data-testid="safety-admin">
      <TopBar left="back" onBack={() => go('maint')} title={S.safetyAdmin} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">
        {err && <div class="banner rosso"><Icon name="errore" /><div>{err}</div></div>}
        <div class="banner info"><Icon name="info" /><div>{S.safetyNotRegister}</div></div>
        {!data ? <p>{S.maintLoading}</p> : (
          <>
            <h2 class="blocco-tit">{S.mapsTitle}</h2>
            {data.mappe.map((m) => (
              <div class="sez-riga" key={m.id} data-testid="map-row">
                <Icon name="mappa" />
                {edit && edit.id === m.id ? (
                  <div class="form" style={{ minWidth: 0 }}>
                    <div class="campo largo">
                      <label for="mp-nome">{S.mapName}</label>
                      <input id="mp-nome" class="input" maxLength={40} value={edit.nome} onInput={(e) => setEdit({ ...edit, nome: e.currentTarget.value })} />
                    </div>
                    <Chips options={depts} value={edit.reparti} onChange={(v) => setEdit({ ...edit, reparti: v })} label={S.mapDepts} />
                  </div>
                ) : (
                  <div style={{ minWidth: 0 }}>
                    <div class="n">{m.nome}<small>· {S.evTotal(data.eventi.filter((e) => e.mappa === m.id).length).replace(' nel periodo', '')}</small></div>
                    <div class="p">{m.larghezza}×{m.altezza} · {m.reparti.length ? m.reparti.map((r) => deptName[r]).filter(Boolean).join(', ') || S.allDepts : S.allDepts}</div>
                  </div>
                )}
                <div class="b">
                  {edit && edit.id === m.id ? (
                    <>
                      <button class="btn primario" data-testid="map-save" onClick={saveMap}><Icon name="spunta" />{S.save}</button>
                      <button class="btn" onClick={() => setEdit(null)}>{S.cancel}</button>
                    </>
                  ) : (
                    <>
                      <button class="btn" onClick={() => setEdit({ id: m.id, nome: m.nome, reparti: [...m.reparti] })}><Icon name="matita" />{S.edit}</button>
                      <button class="btn" onClick={() => setConfirmMap(m)}><Icon name="cestino" />{S.del}</button>
                    </>
                  )}
                </div>
              </div>
            ))}
            {data.mappe.length < data.limiti.mappe && (
              <div class="form" style={{ marginTop: '16px' }}>
                <div class="campo">
                  <label for="mp-nuovo">{S.mapName}</label>
                  <input id="mp-nuovo" class="input" maxLength={40} value={name} onInput={(e) => setName(e.currentTarget.value)} autocomplete="off" />
                  <span class="aiuto">{S.mapNameHelp}</span>
                </div>
                <div class="campo">
                  <span class="lab">{S.mapAdd}</span>
                  <label class={`btn ${busy ? 'disattivo' : ''}`} style={{ position: 'relative', justifySelf: 'start' }}>
                    <Icon name="carica" />{S.mapPick}
                    <input type="file" accept="image/png,image/jpeg" data-testid="map-input" disabled={busy}
                      style={{ position: 'absolute', width: '1px', height: '1px', opacity: 0, overflow: 'hidden' }}
                      onChange={(e) => {
                        const f = e.currentTarget.files[0];
                        e.currentTarget.value = '';
                        addMap(f);
                      }} />
                  </label>
                  <span class="aiuto">{S.mapHelp}</span>
                </div>
              </div>
            )}
            {mapErr && <div class="banner rosso" data-testid="map-err" style={{ marginTop: '12px' }}><Icon name="errore" /><div>{mapErr}</div></div>}

            {map && (
              <>
                <h2 class="blocco-tit">{S.evList}</h2>
                {data.mappe.length > 1 && (
                  <div class="chips" role="group" aria-label={S.mapsTitle}>
                    {data.mappe.map((m) => (
                      <button key={m.id} class="chip" aria-pressed={m.id === map.id ? 'true' : 'false'} onClick={() => setSel(m.id)}>{m.nome}</button>
                    ))}
                  </div>
                )}
                <p class="piccolo"><Icon name="info" /> {S.evAddHelp}</p>
                <MapView map={{ width: map.larghezza, height: map.altezza, name: map.nome }} src={`/api/maps/${map.immagine}`} markers={markers}
                  testid="admin-map" onPick={(p) => setEv({ mappa: map.id, x: p.x, y: p.y })}
                  onMarker={(m) => setEv(evs.find((e) => e.id === m.id))} />
                {evs.length === 0 ? <p data-testid="events-empty">{S.evListEmpty}</p> : (
                  <div class="lista" style={{ marginTop: '16px' }}>
                    {(all ? evs : evs.slice(0, 20)).map((e) => (
                      <div class="sez-riga" key={e.id} data-testid="event-row">
                        <EvMark type={e.tipo} />
                        <div style={{ minWidth: 0 }}>
                          <div class="n">{S.evType1[e.tipo]}<small>· {e.data.split('-').reverse().join('/')}</small></div>
                          {e.nota && <div class="p">{e.nota}</div>}
                        </div>
                        <div class="b"><button class="btn" onClick={() => setEv(e)}><Icon name="matita" />{S.edit}</button></div>
                      </div>
                    ))}
                    {!all && evs.length > 20 && <button class="btn" onClick={() => setAll(true)}>{S.evMore(evs.length)}</button>}
                  </div>
                )}
              </>
            )}

            <h2 class="blocco-tit">{S.moreSettings}</h2>
            <div class="form">
              <div class="campo">
                <label for="sf-inizio">{S.safetyStart}</label>
                <input id="sf-inizio" type="date" class="input" value={start} max={localDay()} onInput={(e) => setStart(e.currentTarget.value)} />
                <span class="aiuto">{S.safetyStartHelp}</span>
                <button class="btn primario" style={{ justifySelf: 'start' }} data-testid="save-start" onClick={saveStart}><Icon name="spunta" />{S.save}</button>
              </div>
              <div class="campo">
                <span class="lab">{S.exportCsv}</span>
                <button class="btn" style={{ justifySelf: 'start' }} data-testid="export-csv" onClick={exportCsv}><Icon name="documento" />{S.exportCsv}</button>
              </div>
            </div>
          </>
        )}
      </div>
      {ev && <EventDialog ev={ev} onClose={() => setEv(null)} onSaved={() => { setEv(null); setToast(S.saved2); changed(); }} />}
      {confirmMap && (
        <div class="velo">
          <div class="dialogo" role="dialog" aria-modal="true" aria-labelledby="dlg-mp">
            <h2 id="dlg-mp">{S.mapDelQ(confirmMap.nome)}</h2>
            <p>{S.mapDelText}</p>
            <div class="azioni">
              <button class="btn rosso" data-testid="confirm-delete" onClick={() => delMap(confirmMap)}><Icon name="cestino" />{S.del}</button>
              <button class="btn" onClick={() => setConfirmMap(null)}>{S.cancel}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
