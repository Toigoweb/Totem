import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand, StatusPill } from '../components/TopBar.jsx';
import { PinPad, pinResult } from '../components/PinPad.jsx';
import { SectionForm } from './SectionForm.jsx';
import { S } from '../strings.js';
import { app, go, exitMaintenance, maintApi, setToast, getManager } from '../app.js';
import { useStore } from '../store.js';
import { serverReachable } from '../api.js';

const exit = () => {
  exitMaintenance();
  go('home');
};

/** Dopo una modifica il server riscandisce (due passaggi di stabilità): sync ritardate. */
function syncSoon() {
  const m = getManager();
  if (!m) return;
  setTimeout(() => m.syncNow({ manual: true }), 3000);
  setTimeout(() => m.syncNow(), 30000);
  setTimeout(() => m.syncNow(), 60000);
}

/** Controlla periodicamente il server: senza server la configurazione è in sola lettura. */
function useServerOnline() {
  const [online, setOnline] = useState(true);
  useEffect(() => {
    let alive = true;
    const check = () => serverReachable().then((ok) => alive && setOnline(ok));
    check();
    const t = setInterval(check, 15000);
    return () => {
      alive = false;
      clearInterval(t);
    };
  }, []);
  return [online, setOnline];
}

export function Maint() {
  const s = useStore(app);
  const [list, setList] = useState(null);
  const [max, setMax] = useState(8);
  const [online, setOnline] = useServerOnline();
  const [confirm, setConfirm] = useState(null);

  const load = async () => {
    try {
      const r = await maintApi('/api/maint/sections');
      if (r.ok) {
        setList(r.data.sections);
        setMax(r.data.max || 8);
        setOnline(true);
      }
    } catch {
      setOnline(false);
    }
  };
  useEffect(() => {
    load();
  }, [online]);

  const rows = list || s.sections.map((x) => ({ id: x.id, nome: x.name, icona: x.icon, emergenza: x.emergency, percorso: '–', documents: null }));

  const move = async (i, d) => {
    const ids = rows.map((x) => x.id);
    [ids[i], ids[i + d]] = [ids[i + d], ids[i]];
    try {
      await maintApi('/api/maint/sections-order', { method: 'POST', body: { ids } });
      await load();
      syncSoon();
    } catch {
      setOnline(false);
    }
  };

  const del = async (sec) => {
    setConfirm(null);
    try {
      const r = await maintApi(`/api/maint/sections/${sec.id}`, { method: 'DELETE' });
      if (r.ok) syncSoon();
      await load();
    } catch {
      setOnline(false);
    }
  };

  const ro = !online;
  return (
    <div class="screen" data-testid="maint">
      <TopBar left="logo-static" title={S.sectionsConfig} right={<StatusPill />} />
      <MaintBand onExit={exit} />
      <div class="body">
        {ro && <div class="banner rosso" data-testid="maint-offline"><Icon name="nuvolaOff" /><div>{S.maintOffline}</div></div>}
        <div class={ro ? 'disattivo' : ''}>
          {rows.map((sec, i) => (
            <div class="sez-riga" key={sec.id} data-testid="maint-row">
              <Icon name={sec.icona} />
              <div style={{ minWidth: 0 }}>
                <div class="n">
                  {sec.nome}
                  {sec.emergenza && <span class="badge emerg">{S.emergency}</span>}
                  {sec.documents != null && <small>· {S.docCount(sec.documents)}</small>}
                </div>
                <div class="p">{sec.percorso}</div>
              </div>
              <div class="b">
                <button class="btn" aria-label={S.moveUp} disabled={ro || i === 0} onClick={() => move(i, -1)}><Icon name="su" /></button>
                <button class="btn" aria-label={S.moveDown} disabled={ro || i === rows.length - 1} onClick={() => move(i, 1)}><Icon name="giu" /></button>
                <button class="btn" disabled={ro || !list} onClick={() => go('form', { id: sec.id })}><Icon name="matita" />{S.edit}</button>
                <button class="btn" disabled={ro} onClick={() => setConfirm(sec)}><Icon name="cestino" />{S.del}</button>
              </div>
            </div>
          ))}
        </div>
        <div class={`azioni ${ro ? 'disattivo' : ''}`}>
          <button class="btn primario" data-testid="add-section" disabled={ro || rows.length >= max} onClick={() => go('form', {})}>
            <Icon name="piu" />{S.addSection.replace('+ ', '')}
          </button>
          {rows.length >= max && <span class="piccolo">{S.maxSections(max)}</span>}
          <button class="btn" disabled={ro} onClick={() => go('changePin')}><Icon name="lucchetto" />{S.changePin}</button>
          <button class="btn" disabled={ro} data-testid="open-devices" onClick={() => go('devices')}><Icon name="ingranaggio" />{S.devices}</button>
          <button class="btn" disabled={ro} data-testid="open-problems" onClick={() => go('problems')}><Icon name="avviso" />{S.problems}</button>
        </div>
      </div>
      {confirm && (
        <div class="velo">
          <div class="dialogo" role="dialog" aria-modal="true" aria-labelledby="dlg-t">
            <h2 id="dlg-t">{S.deleteQ(confirm.nome)}</h2>
            <p>{S.deleteText}</p>
            <div class="azioni">
              <button class="btn rosso" data-testid="confirm-delete" onClick={() => del(confirm)}><Icon name="cestino" />{S.del}</button>
              <button class="btn" onClick={() => setConfirm(null)}>{S.cancel}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

export function MaintForm({ id }) {
  const [sec, setSec] = useState(id ? undefined : null);
  const [online] = useServerOnline();
  useEffect(() => {
    if (!id) return;
    maintApi('/api/maint/sections').then(
      (r) => setSec((r.data.sections || []).find((x) => x.id === id) || null),
      () => setSec(null),
    );
  }, [id]);
  const title = id ? S.editSection : S.newSection;
  return (
    <div class="screen" data-testid="maint-form">
      <TopBar left="back" onBack={() => go('maint')} title={title} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">
        {!online && <div class="banner rosso"><Icon name="nuvolaOff" /><div>{S.maintOffline}</div></div>}
        {sec === undefined ? <p>{S.maintLoading}</p> : (
          <SectionForm
            section={sec}
            readOnly={!online}
            onCancel={() => go('maint')}
            onSaved={() => {
              setToast(S.saved);
              syncSoon();
              go('maint');
            }}
          />
        )}
      </div>
    </div>
  );
}

/** Cambio PIN: PIN attuale, nuovo PIN, conferma. */
export function ChangePin() {
  const [step, setStep] = useState(0);
  const [cur, setCur] = useState('');
  const [nw, setNw] = useState('');
  const titles = [S.pinCurrentTitle, S.pinNewTitle, S.pinConfirmTitle];
  const sub = `${S.changePin} · ${S.stepOf(step + 1, 3)}`;
  return (
    <PinPad
      title={titles[step]}
      subtitle={sub}
      onCancel={() => go('maint')}
      onSubmit={async (pin) => {
        if (step === 0) {
          setCur(pin);
          setStep(1);
          return { ok: true };
        }
        if (step === 1) {
          setNw(pin);
          setStep(2);
          return { ok: true };
        }
        if (pin !== nw) {
          setStep(1);
          return { error: 'message', text: S.pinMismatch };
        }
        const r = await maintApi('/api/maint/pin', { method: 'POST', body: { current: cur, new: pin } });
        const res = pinResult(r);
        if (res.ok) {
          setToast(S.pinChanged);
          go('maint');
          return res;
        }
        setStep(0);
        return res;
      }}
    />
  );
}
