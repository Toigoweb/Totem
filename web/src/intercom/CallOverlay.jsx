// Chiamata dell'interfono a tutto schermo: in arrivo, in uscita, in conversazione, finita.
// Sta sopra a tutto, anche all'emergenza (si può dover chiamare proprio allora).
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { S } from '../strings.js';
import { app } from '../app.js';
import { useStore } from '../store.js';
import { mmss } from '../format.js';
import { answer, reject, hangup, toggleMute, dismissCall, resumeAudio, endedView } from './client.js';

export function CallOverlay() {
  const call = useStore(app, (s) => s.call);
  const [, tick] = useState(0);
  useEffect(() => {
    if (!call || call.phase !== 'active') return;
    const t = setInterval(() => tick((x) => x + 1), 1000);
    return () => clearInterval(t);
  }, [call && call.phase]);
  if (!call) return null;

  const { phase, dir } = call;
  let badge;
  let top;
  let name = call.label;
  let detail = '';
  if (phase === 'ringing' && dir === 'in') {
    badge = S.icIncoming;
    top = S.icFrom;
    name = call.from;
    detail = call.label && call.label !== call.from ? S.icFor(call.label) : '';
  } else if (phase === 'ringing') {
    badge = S.icTitle;
    top = S.icCalling;
  } else if (phase === 'connecting') {
    badge = S.icTitle;
    top = S.icConnecting;
    if (dir === 'in') name = call.from;
  } else if (phase === 'active') {
    badge = S.icTalking;
    top = mmss((Date.now() - call.answeredAt) / 1000);
    if (dir === 'in') name = call.from;
    else if (call.by && call.by !== call.label) detail = call.by;
  } else {
    badge = S.icTitle;
    if (dir === 'in') name = call.from;
  }
  const ended = phase === 'ended' ? endedView(call) : null;
  const endedText = ended && (dir === 'in' && ended.reason === 'noanswer' ? S.icMissed : S.icEndReason[ended.reason] || S.icEnded);

  return (
    <div class={`chiamata f-${phase}`} role="alertdialog" aria-modal="true" aria-labelledby="ch-n" data-testid="call" data-phase={phase} data-dir={dir}>
      <div class="c">
        <span class="ch-badge"><Icon name="microfono" />{badge}</span>
        {top && <p class="ch-top" data-testid="call-top">{top}</p>}
        <h1 id="ch-n" data-testid="call-name">{name}</h1>
        {detail && <p class="ch-det">{detail}</p>}
        {ended && <p class="ch-esito" role="status" data-testid="call-ended"><Icon name="info" />{endedText}</p>}
        {ended && ended.fallback && (
          <p class="ch-numero" data-testid="call-fallback"><Icon name="telefono" />{S.icFallback(ended.fallback)}</p>
        )}
        {call.needsTap && (
          <button class="btn" onClick={resumeAudio}><Icon name="altoparlante" />{S.icTapToHear}</button>
        )}
        <div class="ch-azioni">
          {phase === 'ringing' && dir === 'in' && (
            <>
              <button class="btn ch-si" data-testid="call-answer" onClick={answer}><Icon name="telefono" />{S.icAnswer}</button>
              <button class="btn ch-no" data-testid="call-reject" onClick={reject}><Icon name="riaggancia" />{S.icReject}</button>
            </>
          )}
          {(phase === 'active' || phase === 'connecting') && (
            <button class="btn ch-muto" aria-pressed={call.muted ? 'true' : 'false'} data-testid="call-mute" onClick={toggleMute}>
              <Icon name={call.muted ? 'microfonoOff' : 'microfono'} />{call.muted ? S.icUnmute : S.icMute}
            </button>
          )}
          {(phase !== 'ended' && !(phase === 'ringing' && dir === 'in')) && (
            <button class="btn ch-giu" data-testid="call-hangup" onClick={hangup}><Icon name="riaggancia" />{S.icHangup}</button>
          )}
          {phase === 'ended' && (
            <button class="btn" data-testid="call-close" onClick={dismissCall}><Icon name="x" />{S.icClose}</button>
          )}
        </div>
        {phase !== 'ended' && <p class="ch-nota">{S.icFootnote}</p>}
      </div>
    </div>
  );
}
