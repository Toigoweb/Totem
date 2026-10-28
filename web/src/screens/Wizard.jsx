import { useEffect, useRef, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar } from '../components/TopBar.jsx';
import { PinPad, pinResult } from '../components/PinPad.jsx';
import { SectionForm } from './SectionForm.jsx';
import { S } from '../strings.js';
import { app, go, enterMaintenance, exitMaintenance, markPaired, startSync, getManager, reloadData, PLATFORM } from '../app.js';
import { useStore } from '../store.js';
import { api, NetworkError } from '../api.js';
import { fmtBytes, mmss } from '../format.js';

// Procedura guidata del primo avvio (e della sostituzione del totem):
// codice di setup → PIN (nuovo o attuale) → prima sezione (se non ce ne sono) → prima sync.

function Steps({ steps, current }) {
  const idx = steps.indexOf(current);
  const label = { code: S.stepCode, pin: S.stepPin, section: S.stepSection, sync: S.stepSync };
  return (
    <div class="passi" data-testid="steps">
      <strong>{S.stepOf(idx + 1, steps.length)}</strong>
      {steps.map((st, i) => (
        <span key={st} class={`passo ${i < idx ? 'fatto' : i === idx ? 'att' : ''}`}>
          {i < idx && <Icon name="check" />}
          {label[st]}
        </span>
      ))}
    </div>
  );
}

export function Wizard({ code: initialCode }) {
  const [step, setStep] = useState('code');
  const [info, setInfo] = useState(null); // { setupToken, pinSet, sections }
  const [firstPin, setFirstPin] = useState(null);
  // postazione: solo codice e prima sincronizzazione; totem: codice, PIN, (sezione), sync
  const steps = info && info.station ? ['code', 'sync'] : ['code', 'pin', ...(info && info.sections > 0 ? [] : ['section']), 'sync'];
  const cancel = () => {
    exitMaintenance();
    go('home');
  };

  if (step === 'pin') {
    const creating = !info.pinSet;
    const title = creating ? (firstPin ? S.pinConfirmTitle : S.pinCreateTitle) : S.pinExistingTitle;
    return (
      <PinPad
        title={title}
        subtitle={`${S.wizardTitle} · ${S.stepOf(2, steps.length)}`}
        onCancel={cancel}
        onSubmit={async (pin) => {
          if (creating && !firstPin) {
            setFirstPin(pin);
            return { ok: true };
          }
          if (creating && pin !== firstPin) {
            setFirstPin(null);
            return { error: 'message', text: S.pinMismatch };
          }
          const r = await api('/api/setup/pin', { method: 'POST', body: { setupToken: info.setupToken, pin, platform: PLATFORM } });
          if (r.status === 401 && r.data.error === 'setup_expired') {
            setStep('code');
            return { ok: true };
          }
          const res = pinResult(r);
          if (res.ok) {
            enterMaintenance(r.data.maintToken);
            await markPaired();
            setStep(info.sections > 0 ? 'sync' : 'section');
          }
          return res;
        }}
      />
    );
  }

  return (
    <div class="screen" data-testid="wizard">
      <TopBar left="logo-static" title={S.wizardTitle} right={null} />
      <div class="body">
        {step === 'code' && (
          <CodeStep steps={steps} initial={initialCode} onCancel={cancel} onOk={async (d) => {
            // codice di postazione o di un totem aggiuntivo: abbinato subito, resta la sync
            if (d.paired) {
              await markPaired(d.role);
              if (d.role === 'telefono') {
                go('home'); // il telefono dell'interfono non scarica documenti
                return;
              }
              setInfo({ station: true });
              setStep('sync');
              return;
            }
            setInfo(d);
            setFirstPin(null);
            setStep('pin');
          }} />
        )}
        {step === 'section' && (
          <>
            <Steps steps={steps} current="section" />
            <SectionForm section={null} submitLabel={S.saveContinue} onSaved={() => setStep('sync')} />
          </>
        )}
        {step === 'sync' && <SyncStep steps={steps} />}
      </div>
    </div>
  );
}

function CodeStep({ steps, onOk, onCancel, initial }) {
  const [code, setCode] = useState(() => fmtCode(initial || ''));
  const [msg, setMsg] = useState(null);
  const [lockedUntil, setLockedUntil] = useState(0);
  const [busy, setBusy] = useState(false);
  const [, tick] = useState(0);
  const input = useRef(null);
  useEffect(() => input.current && input.current.focus(), []);
  const locked = lockedUntil > Date.now();
  useEffect(() => {
    if (!locked) return;
    const t = setInterval(() => tick((x) => x + 1), 500);
    return () => clearInterval(t);
  }, [locked]);

  const submit = async (e) => {
    e && e.preventDefault();
    if (busy || locked || code.replace(/[\s-]/g, '').length < 8) return;
    setBusy(true);
    setMsg(null);
    try {
      const r = await api('/api/setup/code', { method: 'POST', body: { code, platform: PLATFORM } });
      if (r.ok) return onOk(r.data);
      if (r.status === 429) setLockedUntil(Date.now() + (r.data.retryAfterSec || 300) * 1000);
      else if (r.status === 409) setMsg(S.codeUsed);
      else if (r.status === 401) setMsg(r.data.totemPaired ? S.codeWrongPaired(r.data.remaining) : S.codeWrong(r.data.remaining));
      else setMsg(S.genericErr);
    } catch (err) {
      setMsg(err instanceof NetworkError ? S.networkErr : S.genericErr);
    } finally {
      setBusy(false);
    }
  };

  return (
    <form class="wiz" onSubmit={submit}>
      <Steps steps={steps} current="code" />
      <h2>{S.codeTitle}</h2>
      <input ref={input} class="input codice" data-testid="setup-code" value={code} maxLength={9} autocomplete="off" spellcheck={false}
        autocapitalize="characters" onInput={(e) => setCode(e.currentTarget.value.toUpperCase())} aria-label={S.codeTitle} disabled={locked} />
      <p>{S.codeHelp}</p>
      {locked && <div class="banner"><Icon name="orologio" /><div>{S.pinLocked(mmss((lockedUntil - Date.now()) / 1000))}</div></div>}
      {msg && !locked && <div class="banner rosso" role="alert"><Icon name="errore" /><div>{msg}</div></div>}
      <div class="azioni">
        <button class="btn primario" type="submit" data-testid="code-next" disabled={busy || locked}><Icon name="avanti" />{S.next}</button>
        <button class="btn" type="button" onClick={onCancel}>{S.cancel}</button>
      </div>
    </form>
  );
}

/** "K7QM4XPA" → "K7QM-4XPA" (il codice dal QR arriva senza trattino). */
export function fmtCode(c) {
  const n = c.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 8);
  return n.length > 4 ? `${n.slice(0, 4)}-${n.slice(4)}` : n;
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/** Attende che il server abbia letto tutte le cartelle, poi scarica tutto. */
function SyncStep({ steps }) {
  const sync = useStore(app, (s) => s.sync);
  const [phase, setPhase] = useState('server'); // server | download | error
  const [err, setErr] = useState(null);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    (async () => {
      setPhase('server');
      setErr(null);
      startSync();
      try {
        await api('/api/scan', { method: 'POST', body: {} }).catch(() => {});
        const deadline = Date.now() + 5 * 60 * 1000;
        // il server accetta un PDF dopo due scansioni stabili (circa 20 secondi)
        for (;;) {
          if (!alive) return;
          const r = await api('/api/status', { timeoutMs: 8000 });
          if (!r.ok) throw new Error('status');
          const secs = r.data.sections || [];
          const bad = secs.find((x) => x.scan && !x.scan.ok && x.scan.error);
          if (bad) {
            setErr(S.syncShareErr(bad.name, S.shareProblem[bad.scan.error] || S.shareProblem.io));
            setPhase('error');
            return;
          }
          const ready = secs.length > 0 && secs.every((x) => x.scan && x.scan.ok && x.scan.pending === 0);
          if (ready) break;
          if (Date.now() > deadline) throw new Error('timeout');
          await sleep(3000);
        }
        if (!alive) return;
        setPhase('download');
        const m = getManager();
        await m.syncNow(); // eventuale sync già in corso con un manifest vecchio
        const res = await m.syncNow();
        if (!alive) return;
        if (!res) {
          const e = m.getState().lastError;
          setErr(e && e.code === 'offline' ? S.networkErr : S.errorBanner(S.failReason[e && e.code] || S.failReason.server));
          setPhase('error');
          return;
        }
        await reloadData();
        exitMaintenance();
        go('home');
      } catch (e) {
        if (!alive) return;
        setErr(e instanceof NetworkError ? S.networkErr : S.genericErr);
        setPhase('error');
      }
    })();
    return () => {
      alive = false;
    };
  }, [attempt]);

  const p = sync.progress;
  const pct = p && p.bytesTotal ? Math.round((100 * p.bytesDone) / p.bytesTotal) : p && p.total ? Math.round((100 * p.done) / p.total) : 0;
  return (
    <div class="wiz" data-testid="wizard-sync">
      <Steps steps={steps} current="sync" />
      <h2>{S.stepSync}</h2>
      {phase === 'error' ? (
        <>
          <div class="banner rosso" role="alert"><Icon name="errore" /><div>{err}</div></div>
          <div class="azioni"><button class="btn primario" data-testid="retry" onClick={() => setAttempt((a) => a + 1)}><Icon name="sync" />{S.retry}</button></div>
        </>
      ) : (
        <>
          <div class="progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" aria-valuenow={pct}>
            <div style={{ width: `${phase === 'server' ? 3 : Math.max(3, pct)}%` }} />
          </div>
          <p style={{ fontWeight: 600 }}>
            {phase === 'server' || !p || !p.total ? S.syncWaitServer : S.syncProgress(p.done, p.total, fmtBytes(p.bytesDone), fmtBytes(p.bytesTotal))}
          </p>
          <p class="piccolo">{S.syncHomeNote}</p>
        </>
      )}
    </div>
  );
}
