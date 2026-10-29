import { describe, it, expect } from 'vitest';
import { reduceCall, endedView } from './client.js';
import { reducePtt } from './ptt.js';
import { b64ToBytes } from './push.js';
import { qrPath } from '../components/Qr.jsx';
import { fmtCode } from '../screens/Wizard.jsx';

const incoming = { type: 'incoming', call: { id: 'c1', from: { id: 'd1', name: 'Totem Magazzino' }, label: 'Primo soccorso' } };

describe('interfono: stato della chiamata', () => {
  it('chiamata in arrivo, poi chiusa da un altro membro del gruppo', () => {
    let c = reduceCall(null, incoming);
    expect(c).toMatchObject({ id: 'c1', dir: 'in', phase: 'ringing', from: 'Totem Magazzino', label: 'Primo soccorso' });
    // una seconda chiamata non sostituisce quella in corso
    expect(reduceCall(c, { ...incoming, call: { ...incoming.call, id: 'c2' } })).toBe(c);
    c = reduceCall(c, { type: 'ended', callId: 'c1', reason: 'elsewhere' });
    expect(c.phase).toBe('ended');
    expect(endedView(c)).toEqual({ reason: 'elsewhere', fallback: '' });
  });

  it('in uscita: risposta, poi nessuna risposta con il numero di ripiego', () => {
    const out = { id: 'c9', dir: 'out', phase: 'ringing', label: 'Portineria', number: '', reason: '' };
    const acc = reduceCall(out, { type: 'accepted', call: { id: 'c9', by: { name: 'Postazione portineria' } } });
    expect(acc).toMatchObject({ phase: 'connecting', by: 'Postazione portineria' });
    expect(reduceCall(out, { type: 'accepted', call: { id: 'altra' } })).toBe(out);
    const na = reduceCall(out, { type: 'ended', callId: 'c9', reason: 'noanswer', call: { number: '3210' } });
    expect(endedView(na)).toEqual({ reason: 'noanswer', fallback: '3210' });
    // chi riaggancia da sé non vede il numero
    expect(endedView({ ...na, reason: 'hangup' }).fallback).toBe('');
  });
});

describe('interfono: utilità', () => {
  it('chiave VAPID base64url in byte', () => {
    const b = b64ToBytes('BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4');
    expect(b.length).toBe(65);
    expect(b[0]).toBe(4);
  });
  it('codice dal QR con il trattino', () => {
    expect(fmtCode('k7qm4xpa')).toBe('K7QM-4XPA');
    expect(fmtCode('K7Q')).toBe('K7Q');
  });
  it('QR con margine e moduli', () => {
    const q = qrPath('https://totem.example/?abbina=K7QM4XPA');
    expect(q.n).toBeGreaterThanOrEqual(29);
    expect(q.d.startsWith('M')).toBe(true);
  });
});

describe('walkie-talkie', () => {
  it('chi parla arriva con ptt-start e se ne va con il suo ptt-end', () => {
    const start = { type: 'ptt-start', talk: { id: 't1', channel: 'ch1', channelName: 'Magazzino', speaker: { id: 'd1', name: 'Totem Magazzino' } } };
    const p = reducePtt(null, start);
    expect(p).toMatchObject({ talkId: 't1', channelName: 'Magazzino', speaker: 'Totem Magazzino', me: false });
    expect(reducePtt(p, { type: 'ptt-end', talkId: 'altro' })).toBe(p);
    expect(reducePtt(p, { type: 'ptt-end', talkId: 't1' })).toBe(null);
  });
});
