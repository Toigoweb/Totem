// Formati di data e dimensioni in italiano.
const pad = (n) => String(n).padStart(2, '0');

export function fmtDate(d) {
  if (!d) return '–';
  const x = new Date(d);
  if (isNaN(x)) return '–';
  return `${pad(x.getDate())}/${pad(x.getMonth() + 1)}/${x.getFullYear()}`;
}

export function fmtTime(d) {
  if (!d) return '–';
  const x = new Date(d);
  return `${pad(x.getHours())}:${pad(x.getMinutes())}`;
}

export function sameDay(a, b) {
  const x = new Date(a);
  const y = new Date(b);
  return x.getFullYear() === y.getFullYear() && x.getMonth() === y.getMonth() && x.getDate() === y.getDate();
}

/** "oggi 08:15" oppure "19/10/2026 08:15" */
export function fmtWhen(d, now = new Date()) {
  if (!d) return null;
  return sameDay(d, now) ? `oggi ${fmtTime(d)}` : `${fmtDate(d)} ${fmtTime(d)}`;
}

/** giorni interi trascorsi */
export function daysSince(d, now = new Date()) {
  if (!d) return Infinity;
  return Math.floor((now - new Date(d)) / 86400000);
}

export function fmtBytes(n) {
  if (n == null || isNaN(n)) return '–';
  if (n < 1024 * 1024) return `${Math.max(1, Math.round(n / 1024))} KB`;
  if (n < 1024 ** 3) return `${Math.round(n / 1024 / 1024)} MB`;
  return `${(n / 1024 ** 3).toFixed(1).replace('.', ',')} GB`;
}

export function mmss(sec) {
  const s = Math.max(0, Math.ceil(sec));
  return `${Math.floor(s / 60)}:${pad(s % 60)}`;
}
