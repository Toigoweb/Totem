// Walkie-talkie: pulsante "tieni premuto" e fascia con chi sta parlando (su ogni schermata).
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { S } from '../strings.js';
import { app } from '../app.js';
import { useStore } from '../store.js';
import { pressTalk, releaseTalk } from './ptt.js';

const MAX_S = 60;

/** Pulsante da tenere premuto (dito, guanti, mouse). Rilasciarlo ovunque chiude il canale. */
export function PttButton({ channel, name, label = S.pttHold, disabled, small }) {
  const ptt = useStore(app, (s) => s.ptt);
  const mine = ptt && ptt.me && ptt.channel === channel && ptt.phase !== 'error';
  const down = (e) => {
    if (disabled || (e.pointerType === 'mouse' && e.button !== 0)) return;
    e.preventDefault();
    try {
      e.currentTarget.setPointerCapture(e.pointerId);
    } catch {
      // non supportato: resta il rilascio sul pulsante
    }
    pressTalk(channel, name);
  };
  return (
    <button type="button" class={`btn ptt-btn ${mine ? 'attivo' : ''} ${small ? 'piccolo' : ''}`} disabled={disabled} aria-pressed={mine ? 'true' : 'false'}
      data-testid="ptt-hold" data-channel={channel} onPointerDown={down} onPointerUp={releaseTalk} onPointerCancel={releaseTalk}
      onLostPointerCapture={releaseTalk} onContextMenu={(e) => e.preventDefault()}>
      <Icon name="microfono" />{mine ? (ptt.phase === 'starting' ? S.pttStarting : S.pttTalking) : label}
    </button>
  );
}

export function PttBar() {
  const ptt = useStore(app, (s) => s.ptt);
  const last = useStore(app, (s) => s.pttLast);
  const call = useStore(app, (s) => s.call);
  const [, tick] = useState(0);
  useEffect(() => {
    if (!ptt || !ptt.me || ptt.phase !== 'talking') return;
    const t = setInterval(() => tick((x) => x + 1), 1000);
    return () => clearInterval(t);
  }, [ptt && ptt.talkId]);
  if (call || (!ptt && !last)) return null;

  // stessa struttura in tutti gli stati: il pulsante "rispondi" non si smonta mentre lo si
  // tiene premuto (si perderebbe la pressione)
  let state;
  let icon = 'altoparlante';
  let title;
  let detail;
  let btn = null;
  if (ptt && ptt.me && ptt.phase === 'error') {
    state = 'error';
    icon = 'avviso';
    title = ptt.reason === 'busy' ? S.pttBusy(ptt.speaker) : S.pttErr[ptt.reason] || S.pttErr.error;
  } else if (ptt && ptt.me) {
    state = 'talking';
    icon = 'microfono';
    const left = ptt.started ? Math.max(0, MAX_S - Math.floor((Date.now() - ptt.started) / 1000)) : MAX_S;
    title = ptt.phase === 'starting' ? S.pttStarting : S.pttTalking;
    detail = ptt.phase === 'talking' ? `${ptt.channelName} · ${S.pttOnline(ptt.listeners)} · ${S.pttLeft(left)}` : ptt.channelName;
    if (last && last.channel === ptt.channel) btn = last;
  } else if (ptt) {
    state = 'listening';
    title = S.pttListening(ptt.speaker);
    detail = S.pttOnChannel(ptt.channelName);
  } else {
    state = 'last';
    title = S.pttLast(last.speaker);
    detail = S.pttOnChannel(last.channelName);
    btn = last;
  }
  return (
    <div class={`ptt-fascia s-${state}`} role={state === 'error' ? 'alert' : 'status'} data-testid="ptt-bar" data-state={state} data-audio={ptt && ptt.audio ? '1' : '0'}>
      <Icon name={icon} />
      <strong data-testid="ptt-title">{title}</strong>
      {detail && <span>{detail}</span>}
      {btn && <PttButton channel={btn.channel} name={btn.channelName} label={S.pttHoldReply} small />}
    </div>
  );
}
