// Chiamate al server. Un errore di rete diventa NetworkError: l'interfaccia lo mostra
// come "server non raggiungibile" e mette la configurazione in sola lettura.

export class NetworkError extends Error {
  constructor() {
    super('rete');
    this.code = 'network';
  }
}

export async function api(path, { method = 'GET', body, maint, timeoutMs = 45000 } = {}) {
  const headers = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (maint) headers['X-Maint-Token'] = maint;
  const ctl = new AbortController();
  const t = setTimeout(() => ctl.abort(), timeoutMs);
  let res;
  try {
    res = await fetch(path, { method, headers, body: body !== undefined ? JSON.stringify(body) : undefined, credentials: 'same-origin', cache: 'no-store', signal: ctl.signal });
  } catch {
    throw new NetworkError();
  } finally {
    clearTimeout(t);
  }
  let data = null;
  try {
    data = await res.json();
  } catch {
    data = null;
  }
  return { status: res.status, ok: res.ok, data: data || {} };
}

/** true se il server risponde entro pochi secondi */
export async function serverReachable() {
  try {
    const r = await api('/api/health', { timeoutMs: 4000 });
    return r.ok;
  } catch {
    return false;
  }
}
