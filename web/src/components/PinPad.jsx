import { useEffect, useRef, useState } from 'preact/hooks';
import { Icon } from './Icon.jsx';
import { S } from '../strings.js';
import { mmss } from '../format.js';
import { serverReachable } from '../api.js';

/**
 * Tastierino PIN a schermo pieno (navy), 4-6 cifre. Funziona anche da tastiera fisica:
 * cifre, Invio, Backspace, Esc.
 * onSubmit(pin) → { ok } | { error: 'wrong', remaining } | { error: 'locked', retryAfterSec }
 *                 | { error: 'network' } | { error: 'message', text }
 */
export function PinPad({ title, subtitle, onSubmit, onCancel, requireServer = true }) {
  const [pin, setPin] = useState('');
  const [msg, setMsg] = useState(null); // { kind: 'err'|'info', icon, text }
  const [lockedUntil, setLockedUntil] = useState(0);
  const [noServer, setNoServer] = useState(false);
  const [busy, setBusy] = useState(false);
  const [, tick] = useState(0);
  const pinRef = useRef('');
  pinRef.current = pin;

  useEffect(() => {
    if (!requireServer) return;
    serverReachable().then((ok) => setNoServer(!ok));
  }, []);

  const now = Date.now();
  const locked = lockedUntil > now;
  useEffect(() => {
    if (!locked) return;
    const t = setInterval(() => tick((x) => x + 1), 500);
    return () => clearInterval(t);
  }, [locked]);
  const disabled = locked || noServer || busy;

  const submit = async (value = pinRef.current) => {
    if (disabled || value.length < 4) return;
    setBusy(true);
    setMsg({ kind: 'info', icon: 'orologio', text: S.pinChecking });
    let r;
    try {
      r = await onSubmit(value);
    } catch {
      r = { error: 'network' };
    }
    setBusy(false);
    setPin('');
    if (!r || r.ok) {
      setMsg(null);
      return;
    }
    if (r.error === 'wrong') setMsg({ kind: 'err', icon: 'errore', text: S.pinWrong(r.remaining) });
    else if (r.error === 'locked') {
      setLockedUntil(Date.now() + r.retryAfterSec * 1000);
      setMsg(null);
    } else if (r.error === 'network') {
      setNoServer(true);
      setMsg(null);
    } else setMsg({ kind: 'err', icon: 'errore', text: r.text || S.genericErr });
  };

  const press = (d) => {
    if (disabled) return;
    setMsg(null);
    setPin((p) => (p.length < 6 ? p + d : p));
  };
  const del = () => !disabled && setPin((p) => p.slice(0, -1));

  useEffect(() => {
    const k = (e) => {
      if (/^[0-9]$/.test(e.key)) press(e.key);
      else if (e.key === 'Backspace') del();
      else if (e.key === 'Enter') submit();
      else if (e.key === 'Escape') onCancel();
      else return;
      e.preventDefault();
    };
    window.addEventListener('keydown', k);
    return () => window.removeEventListener('keydown', k);
  });

  let status = msg;
  if (noServer) status = { kind: 'info', icon: 'nuvolaOff', text: S.pinNoServer };
  else if (locked) status = { kind: 'info', icon: 'orologio', text: S.pinLocked(mmss((lockedUntil - now) / 1000)) };

  return (
    <div class="screen" data-testid="pinpad">
      <div class="pin"><div class="pin-box">
        {subtitle && <div>{subtitle}</div>}
        <h1>{title}</h1>
        <div class="pallini" aria-label={`${pin.length} cifre`}>
          {[0, 1, 2, 3, 4, 5].map((i) => <span key={i} class={i < pin.length ? 'p' : i >= 4 ? 'opz' : ''} />)}
        </div>
        <div class={`msg-pin ${status ? status.kind : 'vuoto'}`} role="status" data-testid="pin-msg">
          {status && <><Icon name={status.icon} />{status.text}</>}
        </div>
        <div class="tastierino">
          {['1', '2', '3', '4', '5', '6', '7', '8', '9'].map((d) => <button key={d} disabled={disabled} onClick={() => press(d)}>{d}</button>)}
          <button class="sm" disabled={disabled} onClick={del}>{S.pinDelete}</button>
          <button disabled={disabled} onClick={() => press('0')}>0</button>
          <button class="ok sm" disabled={disabled || pin.length < 4} data-testid="pin-ok" onClick={() => submit()}>{S.ok}</button>
        </div>
        <button class="btn annulla" onClick={onCancel}>{S.cancel}</button>
      </div></div>
    </div>
  );
}

/** Interpreta la risposta del server a una verifica di PIN o codice. */
export function pinResult(r) {
  if (r.ok) return { ok: true };
  if (r.status === 429) return { error: 'locked', retryAfterSec: r.data.retryAfterSec || 300 };
  if (r.status === 401 && r.data.remaining != null) return { error: 'wrong', remaining: r.data.remaining };
  return { error: 'message', text: S.genericErr };
}
