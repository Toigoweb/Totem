// Push-to-talk (walkie-talkie) sui canali dell'interfono.
//
// Chi tiene premuto apre il microfono e manda l'audio a ciascun membro collegato del
// canale (una connessione WebRTC per ascoltatore, solo in rete interna). Chi ascolta
// riceve soltanto: il suo microfono resta chiuso. Rilasciato il pulsante, tutto si chiude.
import { app } from '../app.js';
import { api } from '../api.js';
import { beep } from './tone.js';

const LAST_VISIBLE_MS = 10000;

let local = null; // microfono di chi parla
let pcs = new Map(); // id del dispositivo → RTCPeerConnection
let pending = new Map(); // candidati arrivati prima della descrizione remota
let audios = new Map();
let pressed = false;
let lastTimer = null;

const cur = () => app.get().ptt;
const setPtt = (ptt) => app.set({ ptt });

/** Stato del walkie-talkie dopo un evento del server. Funzione pura (Vitest). */
export function reducePtt(ptt, ev) {
  if (ev.type === 'ptt-start') {
    const t = ev.talk;
    return { talkId: t.id, channel: t.channel, channelName: t.channelName, speaker: t.speaker.name, speakerId: t.speaker.id, me: false };
  }
  if (ev.type === 'ptt-end' && ptt && ptt.talkId === ev.talkId) return null;
  return ptt;
}

export function onPttEvent(ev) {
  if (ev.type === 'ptt-signal') {
    onSignal(ev.talkId, ev.from, ev.data);
    return;
  }
  const before = cur();
  if (before && before.me && ev.type === 'ptt-end' && before.talkId === ev.talkId) {
    // scaduto il tempo massimo: si smette di trasmettere
    stopSending(before, false);
    return;
  }
  if (before && before.me) return; // si sta parlando: gli altri canali aspettano
  const after = reducePtt(before, ev);
  if (after === before) return;
  if (ev.type === 'ptt-start') {
    closeAll();
    beep('in');
    app.set({ lastInteraction: Date.now() });
  } else {
    closeAll();
    beep('out');
    remember(before);
  }
  setPtt(after);
}

function remember(p) {
  if (!p) return;
  clearTimeout(lastTimer);
  app.set({ pttLast: { channel: p.channel, channelName: p.channelName, speaker: p.speaker } });
  lastTimer = setTimeout(() => app.set({ pttLast: null }), LAST_VISIBLE_MS);
}

async function post(path, body = {}) {
  try {
    return await api(path, { method: 'POST', body, timeoutMs: 8000 });
  } catch {
    return { ok: false, status: 0, data: {} };
  }
}

/** Pressione del pulsante: prende la parola sul canale. */
export async function pressTalk(channel, channelName) {
  const c = app.get().call;
  if (pressed || (c && c.phase !== 'ended') || (cur() && !cur().me)) return;
  pressed = true;
  setPtt({ me: true, phase: 'starting', channel, channelName });
  try {
    local = await navigator.mediaDevices.getUserMedia({ audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true }, video: false });
  } catch {
    pressed = false;
    setPtt({ me: true, phase: 'error', channel, channelName, reason: 'mic' });
    setTimeout(() => cur() && cur().phase === 'error' && setPtt(null), 4000);
    return;
  }
  const r = await post(`/api/intercom/channels/${encodeURIComponent(channel)}/talk`);
  if (!r.ok) {
    releaseMic();
    pressed = false;
    setPtt({ me: true, phase: 'error', channel, channelName, reason: r.status === 409 ? 'busy' : 'error', speaker: r.data.speaker || '' });
    setTimeout(() => cur() && cur().phase === 'error' && setPtt(null), 4000);
    return;
  }
  const t = r.data.talk;
  const p = { me: true, phase: 'talking', talkId: t.id, channel, channelName, listeners: t.listeners.length, started: Date.now() };
  setPtt(p);
  beep('in');
  if (!pressed) {
    stopSending(p, true); // rilasciato mentre si prendeva la parola
    return;
  }
  for (const to of t.listeners) offer(t.id, to);
}

/** Rilascio del pulsante. */
export function releaseTalk() {
  pressed = false;
  const p = cur();
  if (p && p.me && p.phase === 'talking') stopSending(p, true);
}

function stopSending(p, notify) {
  pressed = false;
  closeAll();
  releaseMic();
  setPtt(null);
  beep('out');
  if (notify) post(`/api/intercom/talks/${encodeURIComponent(p.talkId)}/release`);
}

function newPc(talkId, peer) {
  const pc = new RTCPeerConnection({ iceServers: [] });
  pc.onicecandidate = (e) => {
    if (e.candidate) post(`/api/intercom/talks/${encodeURIComponent(talkId)}/signal`, { to: peer, data: { type: 'ice', candidate: e.candidate.toJSON() } });
  };
  pcs.set(peer, pc);
  pending.set(peer, []);
  return pc;
}

async function offer(talkId, to) {
  try {
    const pc = newPc(talkId, to);
    local.getTracks().forEach((tr) => pc.addTrack(tr, local));
    const o = await pc.createOffer();
    await pc.setLocalDescription(o);
    await post(`/api/intercom/talks/${encodeURIComponent(talkId)}/signal`, { to, data: { type: 'offer', sdp: o.sdp } });
  } catch {
    // un ascoltatore che non si collega non ferma gli altri
  }
}

async function onSignal(talkId, from, d) {
  if (!d) return;
  try {
    if (d.type === 'offer') {
      const p = cur();
      if (!p || p.me || p.talkId !== talkId) return;
      const pc = newPc(talkId, from);
      pc.ontrack = (e) => {
        const a = new Audio();
        a.autoplay = true;
        a.srcObject = e.streams[0];
        audios.set(from, a);
        a.play().catch(() => {});
        const k = cur();
        if (k && k.talkId === talkId) setPtt({ ...k, audio: true }); // l'audio arriva davvero
      };
      await pc.setRemoteDescription({ type: 'offer', sdp: d.sdp });
      await flush(from);
      const ans = await pc.createAnswer();
      await pc.setLocalDescription(ans);
      await post(`/api/intercom/talks/${encodeURIComponent(talkId)}/signal`, { to: from, data: { type: 'answer', sdp: ans.sdp } });
    } else if (d.type === 'answer') {
      const pc = pcs.get(from);
      if (pc) {
        await pc.setRemoteDescription({ type: 'answer', sdp: d.sdp });
        await flush(from);
      }
    } else if (d.type === 'ice') {
      const pc = pcs.get(from);
      if (!pc) return;
      if (pc.remoteDescription) await pc.addIceCandidate(d.candidate);
      else pending.get(from).push(d.candidate);
    }
  } catch {
    // audio non collegato con questo dispositivo
  }
}

async function flush(peer) {
  const pc = pcs.get(peer);
  const list = pending.get(peer) || [];
  pending.set(peer, []);
  for (const c of list) await pc.addIceCandidate(c).catch(() => {});
}

function closeAll() {
  for (const pc of pcs.values()) pc.close();
  for (const a of audios.values()) a.srcObject = null;
  pcs = new Map();
  pending = new Map();
  audios = new Map();
}

function releaseMic() {
  if (local) local.getTracks().forEach((t) => t.stop());
  local = null;
}

/** Interfono spento o pagina nascosta: si smette di trasmettere. */
export function stopPtt() {
  const p = cur();
  if (p && p.me && p.phase === 'talking') stopSending(p, true);
  else {
    closeAll();
    setPtt(null);
  }
}

// app in secondo piano (telefono bloccato, cambio di app): si smette di trasmettere
if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) releaseTalk();
  });
}
