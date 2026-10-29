// Interfono lato app: flusso di eventi dal server, chiamate e audio WebRTC.
//
// Parte solo se l'interfono è attivo per questo dispositivo (campo intercom del manifest,
// o rubrica disponibile sul telefono): spento, non apre connessioni e non chiede il
// microfono. Il microfono si chiede solo quando si chiama o si risponde.
//
// L'audio va diretto tra i due browser in rete interna: nessun server STUN/TURN esterno.
import { app } from '../app.js';
import { api } from '../api.js';
import { startTone, stopTone } from './tone.js';
import { onPttEvent, stopPtt } from './ptt.js';

const CONNECT_TIMEOUT_MS = 15000;
const ENDED_VISIBLE_MS = 20000;

/**
 * Stato della chiamata dopo un evento del server. Funzione pura (si prova con Vitest).
 * call: { id, dir: 'out'|'in', phase: 'ringing'|'connecting'|'active'|'ended', label, from, number, reason }
 */
export function reduceCall(call, ev) {
  switch (ev.type) {
    case 'incoming': {
      if (call && call.phase !== 'ended') return call; // già in chiamata
      const c = ev.call;
      return { id: c.id, dir: 'in', phase: 'ringing', label: c.label, from: c.from.name, number: '', reason: '' };
    }
    case 'accepted':
      if (!call || call.id !== ev.call.id || call.dir !== 'out') return call;
      return { ...call, phase: 'connecting', by: ev.call.by ? ev.call.by.name : '' };
    case 'ended':
      if (!call || call.id !== ev.callId || call.phase === 'ended') return call;
      return { ...call, phase: 'ended', reason: ev.reason, number: (ev.call && ev.call.number) || call.number || '' };
    default:
      return call;
  }
}

/** Esito da mostrare quando la chiamata è finita: il motivo e il numero solo se serve. */
export function endedView(call) {
  const fallback = call.dir === 'out' && ['noanswer', 'rejected', 'offline', 'media'].includes(call.reason) ? call.number : '';
  return { reason: call.reason || 'hangup', fallback };
}

// ---------------------------------------------------------------------------

let es = null;
let pc = null;
let local = null;
let pendingIce = [];
let audioEl = null;
let connectTimer = null;
let endedTimer = null;
let wanted = null; // { id, answer } da una notifica del telefono

const setCall = (call) => app.set({ call });
const cur = () => app.get().call;

export function intercomRunning() {
  return !!es;
}

/** Apre il flusso di eventi (idempotente). */
export function startIntercom() {
  if (es || typeof EventSource === 'undefined') return;
  es = new EventSource('/api/intercom/events');
  es.onmessage = (m) => {
    let ev;
    try {
      ev = JSON.parse(m.data);
    } catch {
      return;
    }
    onEvent(ev);
  };
  es.onerror = () => {
    // 404 (interfono spento) chiude il flusso per sempre; altrimenti riprova da solo
    if (es && es.readyState === 2) {
      es = null;
      app.set({ intercomLive: false });
    }
  };
  es.onopen = () => app.set({ intercomLive: true });
}

export function stopIntercom() {
  if (es) es.close();
  es = null;
  app.set({ intercomLive: false });
  stopPtt();
  const c = cur();
  if (c && c.phase !== 'ended') hangup();
}

function onEvent(ev) {
  if (ev.type.startsWith('ptt-')) {
    onPttEvent(ev);
    return;
  }
  if (ev.type === 'signal') {
    onSignal(ev.data);
    return;
  }
  const before = cur();
  const after = reduceCall(before, ev);
  if (after === before) return;
  setCall(after);
  if (ev.type === 'incoming') {
    app.set({ lastInteraction: Date.now() }); // sveglia il totem dal salvaschermo
    startTone('ring');
    if (wanted && wanted.id === after.id) {
      const w = wanted;
      wanted = null;
      if (w.answer) answer();
    }
  } else if (ev.type === 'accepted') {
    stopTone();
    connect(true);
  } else if (ev.type === 'ended') {
    finish();
  }
}

/** Da una notifica del telefono: rispondi (o mostra) la chiamata indicata. */
export function wantCall(id, answerNow) {
  const c = cur();
  if (c && c.id === id && c.phase === 'ringing' && c.dir === 'in') {
    if (answerNow) answer();
    return;
  }
  wanted = { id, answer: answerNow };
}

async function getMic() {
  if (local) return local;
  local = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }, video: false });
  return local;
}

/** Chiama un ruolo ("group:<id>") o un dispositivo ("device:<id>"). */
export async function call(target, label) {
  const c = cur();
  if (c && c.phase !== 'ended') return;
  clearTimeout(endedTimer);
  try {
    await getMic(); // il permesso si chiede adesso, con il tocco di chi chiama
  } catch {
    setCall({ id: '', dir: 'out', phase: 'ended', label, reason: 'mic', number: '' });
    return;
  }
  let r;
  try {
    r = await api('/api/intercom/calls', { method: 'POST', body: { target }, timeoutMs: 10000 });
  } catch {
    r = { ok: false, status: 0, data: {} };
  }
  if (!r.ok) {
    releaseMic();
    setCall({ id: '', dir: 'out', phase: 'ended', label, reason: r.status === 409 ? 'busy' : 'error', number: '' });
    scheduleClear();
    return;
  }
  const k = r.data.call;
  if (k.state === 'ended') {
    releaseMic();
    setCall({ id: k.id, dir: 'out', phase: 'ended', label: k.label, reason: r.data.reason || 'offline', number: k.number || '' });
    scheduleClear();
    return;
  }
  setCall({ id: k.id, dir: 'out', phase: 'ringing', label: k.label, number: k.number || '', reason: '' });
  startTone('ringback');
}

export async function answer() {
  const c = cur();
  if (!c || c.dir !== 'in' || c.phase !== 'ringing') return;
  stopTone();
  setCall({ ...c, phase: 'connecting' });
  try {
    await getMic();
  } catch {
    await post(c.id, 'reject');
    setCall({ ...c, phase: 'ended', reason: 'mic' });
    finish();
    return;
  }
  const r = await post(c.id, 'answer');
  if (!r.ok) {
    setCall({ ...c, phase: 'ended', reason: r.status === 404 ? 'hangup' : 'elsewhere' });
    finish();
    return;
  }
  connect(false);
}

export async function reject() {
  const c = cur();
  if (!c) return;
  stopTone();
  setCall(null);
  await post(c.id, 'reject');
}

export async function hangup() {
  const c = cur();
  if (!c) return;
  if (c.phase === 'ended') {
    setCall(null);
    return;
  }
  setCall({ ...c, phase: 'ended', reason: 'hangup' });
  finish();
  if (c.id) await post(c.id, 'hangup');
}

export function toggleMute() {
  const c = cur();
  if (!c || !local) return;
  const muted = !c.muted;
  local.getAudioTracks().forEach((t) => {
    t.enabled = !muted;
  });
  setCall({ ...c, muted });
}

/** Chiude la schermata della chiamata finita. */
export function dismissCall() {
  const c = cur();
  if (c && c.phase === 'ended') setCall(null);
}

async function post(id, action, body = {}) {
  try {
    return await api(`/api/intercom/calls/${encodeURIComponent(id)}/${action}`, { method: 'POST', body, timeoutMs: 10000 });
  } catch {
    return { ok: false, status: 0, data: {} };
  }
}

// ---------------------------------------------------------------------------
// WebRTC: chi chiama fa l'offerta quando l'altro ha risposto.

function connect(offerer) {
  const c = cur();
  if (!c || pc) return;
  const id = c.id;
  pc = new RTCPeerConnection({ iceServers: [] });
  pendingIce = [];
  local.getTracks().forEach((t) => pc.addTrack(t, local));
  pc.onicecandidate = (e) => {
    if (e.candidate) post(id, 'signal', { type: 'ice', candidate: e.candidate.toJSON ? e.candidate.toJSON() : e.candidate });
  };
  pc.ontrack = (e) => {
    if (!audioEl) {
      audioEl = new Audio();
      audioEl.autoplay = true;
    }
    audioEl.srcObject = e.streams[0];
    audioEl.play().catch(() => {
      const k = cur();
      if (k) setCall({ ...k, needsTap: true });
    });
  };
  pc.onconnectionstatechange = () => {
    const k = cur();
    if (!pc || !k || k.id !== id) return;
    if (pc.connectionState === 'connected') {
      clearTimeout(connectTimer);
      setCall({ ...k, phase: 'active', answeredAt: Date.now() });
    } else if (pc.connectionState === 'failed') {
      mediaFailed(id);
    }
  };
  clearTimeout(connectTimer);
  connectTimer = setTimeout(() => {
    const k = cur();
    if (k && k.id === id && k.phase === 'connecting') mediaFailed(id);
  }, CONNECT_TIMEOUT_MS);
  if (offerer) {
    (async () => {
      const offer = await pc.createOffer();
      await pc.setLocalDescription(offer);
      await post(id, 'signal', { type: 'offer', sdp: offer.sdp });
    })().catch(() => mediaFailed(id));
  }
}

function mediaFailed(id) {
  const k = cur();
  if (!k || k.id !== id) return;
  setCall({ ...k, phase: 'ended', reason: 'media' });
  finish();
  post(id, 'hangup');
}

async function onSignal(d) {
  if (!pc || !d) return;
  try {
    if (d.type === 'offer') {
      await pc.setRemoteDescription({ type: 'offer', sdp: d.sdp });
      await flushIce();
      const ans = await pc.createAnswer();
      await pc.setLocalDescription(ans);
      await post(cur().id, 'signal', { type: 'answer', sdp: ans.sdp });
    } else if (d.type === 'answer') {
      await pc.setRemoteDescription({ type: 'answer', sdp: d.sdp });
      await flushIce();
    } else if (d.type === 'ice') {
      if (pc.remoteDescription) await pc.addIceCandidate(d.candidate);
      else pendingIce.push(d.candidate);
    }
  } catch {
    const k = cur();
    if (k) mediaFailed(k.id);
  }
}

async function flushIce() {
  const list = pendingIce;
  pendingIce = [];
  for (const c of list) await pc.addIceCandidate(c).catch(() => {});
}

/** Tocco per sentire, se il browser ha bloccato l'audio. */
export function resumeAudio() {
  if (audioEl) audioEl.play().catch(() => {});
  const k = cur();
  if (k) setCall({ ...k, needsTap: false });
}

function releaseMic() {
  if (local) local.getTracks().forEach((t) => t.stop());
  local = null;
}

function finish() {
  stopTone();
  clearTimeout(connectTimer);
  if (pc) pc.close();
  pc = null;
  pendingIce = [];
  if (audioEl) audioEl.srcObject = null;
  releaseMic();
  scheduleClear();
}

function scheduleClear() {
  clearTimeout(endedTimer);
  endedTimer = setTimeout(() => {
    const c = cur();
    if (c && c.phase === 'ended') setCall(null);
  }, ENDED_VISIBLE_MS);
}
