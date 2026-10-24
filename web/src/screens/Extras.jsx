// Manutenzione: reparti, numeri utili, avvisi, salvaschermo e monitoraggio.
// Ogni schermata carica le impostazioni, si modifica in locale e si salva con "Salva".
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand, Toggle } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go, exitMaintenance, maintApi, setToast, getManager } from '../app.js';
import { useStore } from '../store.js';

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

/** Carica /api/maint/extras e la lista delle sezioni (per i reparti). */
function useExtras() {
  const [data, setData] = useState(null);
  const [err, setErr] = useState(null);
  const load = async () => {
    try {
      const r = await maintApi('/api/maint/extras');
      if (r.ok) setData(r.data);
      else setErr(S.genericErr);
    } catch {
      setErr(S.maintOffline);
    }
  };
  useEffect(() => {
    load();
  }, []);
  return [data, err, load];
}

/** Dopo un salvataggio il totem riprende i dati alla sync successiva: la si anticipa. */
function syncSoon() {
  const m = getManager();
  if (m) setTimeout(() => m.syncNow(), 1500);
}

async function save(path, body, setErr) {
  setErr(null);
  try {
    const r = await maintApi(path, { method: 'PUT', body });
    if (r.ok) {
      setToast(S.saved2);
      syncSoon();
      return true;
    }
    setErr(r.status === 400 ? S.fieldErr(r.data.field) : S.saveErr);
  } catch {
    setErr(S.maintOffline);
  }
  return false;
}

function ErrBanner({ err }) {
  return err ? <div class="banner rosso" role="alert" data-testid="extras-err"><Icon name="errore" /><div>{err}</div></div> : null;
}

/** Scelta multipla a chip; nessuna scelta = tutti. */
function Chips({ options, value, onChange, label }) {
  if (!options.length) return null;
  const toggle = (id) => onChange(value.includes(id) ? value.filter((x) => x !== id) : [...value, id]);
  return (
    <div class="campo largo">
      <span class="lab">{label}</span>
      <div class="chips">
        {options.map((o) => (
          <button key={o.id} type="button" class="chip" aria-pressed={value.includes(o.id) ? 'true' : 'false'} onClick={() => toggle(o.id)}>
            {value.includes(o.id) && <Icon name="check" />}{o.nome}
          </button>
        ))}
      </div>
    </div>
  );
}

function ListActions({ onAdd, addLabel, canAdd, onSave }) {
  return (
    <div class="azioni">
      <button class="btn" data-testid="add-item" disabled={!canAdd} onClick={onAdd}><Icon name="piu" />{addLabel.replace('+ ', '')}</button>
      <button class="btn primario" data-testid="save-extras" onClick={onSave}><Icon name="spunta" />{S.save2}</button>
    </div>
  );
}

function Row({ children, onRemove }) {
  return (
    <div class="riga-ed" data-testid="extras-row">
      <div class="form">{children}</div>
      <button class="btn" aria-label={S.remove} onClick={onRemove}><Icon name="cestino" /></button>
    </div>
  );
}

// ---------------------------------------------------------------------------

export function Departments() {
  const [data, loadErr] = useExtras();
  const sections = useStore(app, (s) => s.sections);
  const [list, setList] = useState(null);
  const [allSecs, setAllSecs] = useState([]);
  const [err, setErr] = useState(null);
  useEffect(() => {
    if (data) setList(data.reparti.map((d) => ({ ...d })));
  }, [data]);
  useEffect(() => {
    // tutte le sezioni configurate, anche quelle che questo dispositivo non vede
    maintApi('/api/maint/sections').then((r) => setAllSecs((r.data.sections || []).map((x) => ({ id: x.id, nome: x.nome }))), () => {});
  }, []);
  const secs = allSecs.length ? allSecs : sections.map((x) => ({ id: x.id, nome: x.name }));
  const upd = (i, patch) => setList(list.map((d, j) => (j === i ? { ...d, ...patch } : d)));
  return (
    <Frame title={S.departmentsTitle} testid="departments">
      <p class="piccolo">{S.departmentsHelp}</p>
      <ErrBanner err={loadErr || err} />
      {list && list.map((d, i) => (
        <Row key={d.id || `n${i}`} onRemove={() => setList(list.filter((_, j) => j !== i))}>
          <div class="campo largo">
            <label for={`dep-${i}`}>{S.depName}</label>
            <input id={`dep-${i}`} class="input" maxLength={30} value={d.nome} onInput={(e) => upd(i, { nome: e.currentTarget.value })} />
          </div>
          <Chips options={secs} value={d.sezioni || []} onChange={(v) => upd(i, { sezioni: v })} label={S.depSections} />
        </Row>
      ))}
      {list && <ListActions addLabel={S.addDepartment} canAdd={list.length < data.limiti.reparti}
        onAdd={() => setList([...list, { nome: '', sezioni: [] }])}
        onSave={() => save('/api/maint/departments', { reparti: list }, setErr)} />}
    </Frame>
  );
}

export function Contacts() {
  const [data, loadErr] = useExtras();
  const [list, setList] = useState(null);
  const [err, setErr] = useState(null);
  useEffect(() => {
    if (data) setList(data.contatti.map((c) => ({ ...c })));
  }, [data]);
  const upd = (i, patch) => setList(list.map((d, j) => (j === i ? { ...d, ...patch } : d)));
  return (
    <Frame title={S.contactsTitle} testid="contacts-edit">
      <p class="piccolo">{S.contactsHelp}</p>
      <ErrBanner err={loadErr || err} />
      {list && list.map((c, i) => (
        <Row key={c.id || `n${i}`} onRemove={() => setList(list.filter((_, j) => j !== i))}>
          <div class="campo">
            <label for={`cl-${i}`}>{S.contactLabel}</label>
            <input id={`cl-${i}`} class="input" maxLength={40} placeholder={S.contactLabelPh} value={c.etichetta}
              onInput={(e) => upd(i, { etichetta: e.currentTarget.value })} />
          </div>
          <div class="campo">
            <label for={`cn-${i}`}>{S.contactNumber}</label>
            <input id={`cn-${i}`} class="input mono" maxLength={20} inputMode="tel" value={c.numero}
              onInput={(e) => upd(i, { numero: e.currentTarget.value })} />
          </div>
          <Chips options={data.reparti} value={c.reparti || []} onChange={(v) => upd(i, { reparti: v })} label={S.validFor} />
        </Row>
      ))}
      {list && <ListActions addLabel={S.addContact} canAdd={list.length < data.limiti.contatti}
        onAdd={() => setList([...list, { etichetta: '', numero: '', reparti: [] }])}
        onSave={() => save('/api/maint/contacts', { contatti: list }, setErr)} />}
    </Frame>
  );
}

export function Notices() {
  const [data, loadErr] = useExtras();
  const [list, setList] = useState(null);
  const [err, setErr] = useState(null);
  useEffect(() => {
    if (data) setList(data.avvisi.map((n) => ({ ...n })));
  }, [data]);
  const upd = (i, patch) => setList(list.map((d, j) => (j === i ? { ...d, ...patch } : d)));
  return (
    <Frame title={S.noticesTitle} testid="notices-edit">
      <p class="piccolo">{S.noticesHelp}</p>
      <ErrBanner err={loadErr || err} />
      {list && list.map((n, i) => (
        <Row key={n.id || `n${i}`} onRemove={() => setList(list.filter((_, j) => j !== i))}>
          <div class="campo largo">
            <label for={`nt-${i}`}>{S.noticeText}</label>
            <input id={`nt-${i}`} class="input" maxLength={200} value={n.testo} onInput={(e) => upd(i, { testo: e.currentTarget.value })} />
          </div>
          <div class="campo">
            <span class="lab">{S.noticeLevel}</span>
            <div class="seg" role="group">
              <button aria-pressed={n.livello === 'info' ? 'true' : 'false'} onClick={() => upd(i, { livello: 'info' })}>{S.noticeInfo}</button>
              <button aria-pressed={n.livello === 'attenzione' ? 'true' : 'false'} onClick={() => upd(i, { livello: 'attenzione' })}>{S.noticeWarn}</button>
            </div>
          </div>
          <div class="campo">
            <span class="lab">{S.noticeFrom} / {S.noticeTo}</span>
            <div class="azioni">
              <input class="input" type="date" aria-label={S.noticeFrom} value={n.dal || ''} onInput={(e) => upd(i, { dal: e.currentTarget.value })} style={{ width: 'auto' }} />
              <input class="input" type="date" aria-label={S.noticeTo} value={n.al || ''} onInput={(e) => upd(i, { al: e.currentTarget.value })} style={{ width: 'auto' }} />
            </div>
          </div>
          <Chips options={data.reparti} value={n.reparti || []} onChange={(v) => upd(i, { reparti: v })} label={S.validFor} />
        </Row>
      ))}
      {list && <ListActions addLabel={S.addNotice} canAdd={list.length < data.limiti.avvisi}
        onAdd={() => setList([...list, { testo: '', livello: 'info', dal: '', al: '', reparti: [] }])}
        onSave={() => save('/api/maint/notices', { avvisi: list }, setErr)} />}
    </Frame>
  );
}

export function ScreensaverSettings() {
  const [data, loadErr] = useExtras();
  const [f, setF] = useState(null);
  const [err, setErr] = useState(null);
  useEffect(() => {
    if (data) setF({ ...data.salvaschermo });
  }, [data]);
  const [from, to] = f ? f.orarioNero.split('-') : ['', ''];
  return (
    <Frame title={S.screensaverTitle} testid="screensaver-edit">
      <p class="piccolo">{S.screensaverHelp}</p>
      <ErrBanner err={loadErr || err} />
      {f && (
        <div class="form">
          <div class="campo"><span class="lab">{S.ssEnabled}</span><Toggle id="ss-on" on={f.attivo} onChange={(v) => setF({ ...f, attivo: v })} /></div>
          <div class="campo">
            <label for="ss-min">{S.ssAfter}</label>
            <input id="ss-min" class="input" type="number" min="1" max="120" value={f.dopoMinuti}
              onInput={(e) => setF({ ...f, dopoMinuti: parseInt(e.currentTarget.value, 10) || 0 })} />
          </div>
          <div class="campo"><span class="lab">{S.ssNight}</span><Toggle id="ss-night" on={f.notte} onChange={(v) => setF({ ...f, notte: v })} /></div>
          <div class="campo">
            <span class="lab">{S.ssNightHours}</span>
            <div class="azioni">
              <span>{S.ssFrom}</span>
              <input class="input" type="time" aria-label={S.ssFrom} value={from} style={{ width: 'auto' }} onInput={(e) => setF({ ...f, orarioNero: `${e.currentTarget.value}-${to}` })} />
              <span>{S.ssTo}</span>
              <input class="input" type="time" aria-label={S.ssTo} value={to} style={{ width: 'auto' }} onInput={(e) => setF({ ...f, orarioNero: `${from}-${e.currentTarget.value}` })} />
            </div>
          </div>
          <div class="campo largo">
            <div class="azioni"><button class="btn primario" data-testid="save-extras" onClick={() => save('/api/maint/screensaver', f, setErr)}><Icon name="spunta" />{S.save2}</button></div>
          </div>
        </div>
      )}
    </Frame>
  );
}

export function Monitoring() {
  const [data, loadErr, reload] = useExtras();
  const [token, setToken] = useState(null);
  const [soglia, setSoglia] = useState(null);
  const [err, setErr] = useState(null);
  useEffect(() => {
    if (data) setSoglia(data.monitor.sogliaMinuti);
  }, [data]);
  const gen = async () => {
    setErr(null);
    try {
      const r = await maintApi('/api/maint/monitor/token', { method: 'POST', body: {} });
      if (r.ok) {
        setToken(r.data.token);
        reload();
      }
    } catch {
      setErr(S.maintOffline);
    }
  };
  const off = async () => {
    try {
      await maintApi('/api/maint/monitor/token', { method: 'DELETE' });
      setToken(null);
      reload();
    } catch {
      setErr(S.maintOffline);
    }
  };
  const url = `${location.origin}/api/monitor`;
  return (
    <Frame title={S.monitorTitle} testid="monitor-edit">
      <p class="piccolo">{S.monitorHelp}</p>
      <ErrBanner err={loadErr || err} />
      {data && (
        <>
          <div class={`banner ${data.monitor.attivo ? 'info' : ''}`} data-testid="monitor-state">
            <Icon name={data.monitor.attivo ? 'spunta' : 'info'} /><div>{data.monitor.attivo ? S.monitorOn : S.monitorOff}</div>
          </div>
          {token && (
            <div class="banner" data-testid="monitor-token">
              <Icon name="lucchetto" />
              <div style={{ display: 'grid', gap: '8px', minWidth: 0 }}>
                <span>{S.monitorTokenOnce}</span>
                <code class="token" style={{ userSelect: 'all', wordBreak: 'break-all' }}>{token}</code>
                <strong>{S.monitorUsage}</strong>
                <code style={{ userSelect: 'all', wordBreak: 'break-all' }}>{`curl -H "Authorization: Bearer ${token}" ${url}`}</code>
                <code style={{ userSelect: 'all', wordBreak: 'break-all' }}>{`${url}?formato=testo&token=${token}`}</code>
              </div>
            </div>
          )}
          <div class="azioni">
            <button class="btn primario" data-testid="monitor-gen" onClick={gen}><Icon name="lucchetto" />{data.monitor.attivo ? S.monitorRegenerate : S.monitorGenerate}</button>
            {data.monitor.attivo && <button class="btn" onClick={off}>{S.monitorDisable}</button>}
          </div>
          <div class="campo">
            <label for="mon-min">{S.monitorThreshold}</label>
            <div class="azioni">
              <input id="mon-min" class="input" type="number" min="10" style={{ width: '8em' }} value={soglia || ''}
                onInput={(e) => setSoglia(parseInt(e.currentTarget.value, 10) || 0)} />
              <button class="btn" data-testid="save-extras" onClick={() => save('/api/maint/monitor', { sogliaMinuti: soglia }, setErr)}><Icon name="spunta" />{S.save2}</button>
            </div>
          </div>
        </>
      )}
    </Frame>
  );
}
