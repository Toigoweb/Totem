// Suonerie generate con WebAudio (nessun file audio da scaricare).
// 'ring': chiamata in arrivo, doppio squillo forte; 'ringback': attesa per chi chiama.

let ctx = null;
let timer = null;

const PATTERNS = {
  ring: { freqs: [880, 660], on: [0, 0.4, 0.6, 1.0], period: 3000, gain: 0.5 },
  ringback: { freqs: [425], on: [0, 1.0], period: 4000, gain: 0.15 },
};

function ring(p) {
  const t0 = ctx.currentTime;
  for (let i = 0; i + 1 < p.on.length; i += 2) {
    const o = ctx.createOscillator();
    const g = ctx.createGain();
    o.frequency.value = p.freqs[(i / 2) % p.freqs.length];
    g.gain.setValueAtTime(0, t0 + p.on[i]);
    g.gain.linearRampToValueAtTime(p.gain, t0 + p.on[i] + 0.02);
    g.gain.setValueAtTime(p.gain, t0 + p.on[i + 1] - 0.02);
    g.gain.linearRampToValueAtTime(0, t0 + p.on[i + 1]);
    o.connect(g).connect(ctx.destination);
    o.start(t0 + p.on[i]);
    o.stop(t0 + p.on[i + 1] + 0.05);
  }
}

export function startTone(kind) {
  stopTone();
  const p = PATTERNS[kind];
  try {
    if (!ctx) ctx = new (window.AudioContext || window.webkitAudioContext)();
    if (ctx.state === 'suspended') ctx.resume().catch(() => {});
    ring(p);
    timer = setInterval(() => ring(p), p.period);
  } catch {
    // audio non disponibile: resta la schermata
  }
  if (kind === 'ring' && navigator.vibrate) navigator.vibrate([500, 300, 500]);
}

/** Segnale breve del walkie-talkie: 'in' quando si apre il canale, 'out' quando si chiude. */
export function beep(kind) {
  try {
    if (!ctx) ctx = new (window.AudioContext || window.webkitAudioContext)();
    if (ctx.state === 'suspended') ctx.resume().catch(() => {});
    beep1(kind === 'in' ? [880, 1320] : [1320, 880]);
  } catch {
    // audio non disponibile
  }
}

function beep1(freqs) {
  const t0 = ctx.currentTime;
  freqs.forEach((f, i) => {
    const o = ctx.createOscillator();
    const g = ctx.createGain();
    o.frequency.value = f;
    g.gain.setValueAtTime(0.25, t0 + i * 0.09);
    g.gain.linearRampToValueAtTime(0, t0 + i * 0.09 + 0.08);
    o.connect(g).connect(ctx.destination);
    o.start(t0 + i * 0.09);
    o.stop(t0 + i * 0.09 + 0.09);
  });
}

export function stopTone() {
  clearInterval(timer);
  timer = null;
}
