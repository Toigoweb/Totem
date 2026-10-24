import { describe, it, expect } from 'vitest';
import { pillState, recentDocs } from './app.js';
import { inWindow, shouldApply } from './kiosk/updates.js';
import { validPath, validUser } from './screens/SectionForm.jsx';
import { buildIndex } from './screens/Search.jsx';

const base = { sync: { running: false, progress: null, lastError: null }, deviceError: false, settings: { staleDays: 3 }, meta: { lastSuccess: null, lastError: null } };
const now = new Date('2026-10-24T08:20:00');

describe('pillola di stato', () => {
  it('priorità: in corso, abbinamento, mai, vecchio, offline, ok', () => {
    expect(pillState({ ...base, sync: { running: true, progress: { done: 12, total: 40 } } }, now)).toEqual({ kind: 'syncing', done: 12, total: 40 });
    expect(pillState({ ...base, deviceError: true }, now).kind).toBe('device');
    expect(pillState(base, now).kind).toBe('never');
    expect(pillState({ ...base, meta: { lastSuccess: '2026-10-19T08:15:00' } }, now)).toEqual({ kind: 'stale', days: 5 });
    expect(pillState({ ...base, meta: { lastSuccess: '2026-10-24T08:15:00', lastError: { code: 'offline' } } }, now).kind).toBe('offline');
    expect(pillState({ ...base, meta: { lastSuccess: '2026-10-24T08:15:00' } }, now).kind).toBe('ok');
  });
});

describe('aggiornati di recente', () => {
  const d = (id, first, upd, available = true) => ({ id, firstSeen: first, updated: upd, available });
  it('ultimi 30 giorni, al massimo 8, badge corretti', () => {
    const docs = [
      d('nuovo', '2026-10-20T10:00:00Z', '2026-10-20T10:00:00Z'),
      d('agg', '2025-01-01T00:00:00Z', '2026-10-22T10:00:00Z'),
      d('vecchio', '2025-01-01T00:00:00Z', '2026-08-01T00:00:00Z'),
      d('nonscaricato', '2026-10-23T00:00:00Z', '2026-10-23T00:00:00Z', false),
      ...Array.from({ length: 10 }, (_, i) => d(`x${i}`, '2026-10-01T00:00:00Z', '2026-10-01T00:00:00Z')),
    ];
    const r = recentDocs(docs, 30, now);
    expect(r).toHaveLength(8);
    expect(r[0]).toMatchObject({ id: 'agg', badge: 'updated' });
    expect(r[1]).toMatchObject({ id: 'nuovo', badge: 'new' });
    expect(r.find((x) => x.id === 'vecchio' || x.id === 'nonscaricato')).toBeUndefined();
  });
});

describe('aggiornamenti notturni', () => {
  it('finestra, anche a cavallo della mezzanotte', () => {
    expect(inWindow('02:00-05:00', new Date('2026-10-24T03:00:00'))).toBe(true);
    expect(inWindow('02:00-05:00', new Date('2026-10-24T05:00:00'))).toBe(false);
    expect(inWindow('23:00-01:00', new Date('2026-10-24T00:30:00'))).toBe(true);
    expect(inWindow('bah', new Date())).toBe(false);
  });
  it('mai durante la lettura o con interazioni recenti', () => {
    const t = new Date('2026-10-24T03:00:00');
    const s = { settings: { nightWindow: '02:00-05:00', updateIdleMinutes: 15 }, lastInteraction: t.getTime() - 20 * 60000, route: { name: 'home' }, maint: null, sync: { running: false } };
    expect(shouldApply(s, t)).toBe(true);
    expect(shouldApply({ ...s, route: { name: 'reader' } }, t)).toBe(false);
    expect(shouldApply({ ...s, lastInteraction: t.getTime() - 60000 }, t)).toBe(false);
    expect(shouldApply(s, new Date('2026-10-24T12:00:00'))).toBe(false);
  });
});

describe('form sezione', () => {
  it('percorso e utente come il server', () => {
    expect(validPath('\\\\srv\\sds')).toBe(true);
    expect(validPath('\\\\srv\\sds\\Sotto cartella\\')).toBe(true);
    for (const bad of ['srv\\sds', '\\\\srv', '//srv/sds', '\\\\srv\\sds\\..\\x', '\\\\srv\\\\sds']) expect(validPath(bad)).toBe(false);
    expect(validUser('AZIENDA\\mario')).toBe(true);
    expect(validUser('mario@azienda.local')).toBe(true);
    expect(validUser('')).toBe(true);
    expect(validUser('a b')).toBe(false);
  });
});

describe('ricerca locale', () => {
  it('prefisso, accenti e sezioni', () => {
    const docs = [
      { id: 's/a', section: 's', title: 'Acetone', folder: 'Solventi', path: 'Solventi/Acetone_rev3.pdf', rev: '3' },
      { id: 's/b', section: 's', title: 'Diluente nitro', folder: '', path: 'Diluente.pdf', rev: '' },
      { id: 'e/c', section: 'e', title: 'Piano di evacuazione', folder: '', path: 'Piano.pdf', rev: '5' },
    ];
    const ix = buildIndex(docs, [{ id: 's', name: 'Schede di sicurezza' }, { id: 'e', name: 'Piani di emergenza' }]);
    expect(ix.search('aceto').map((r) => r.id)).toEqual(['s/a']);
    expect(ix.search('évacuazione').map((r) => r.id)).toEqual(['e/c']);
    expect(ix.search('solventi').map((r) => r.id)).toEqual(['s/a']);
    expect(ix.search('emergenza').map((r) => r.id)).toEqual(['e/c']);
    expect(ix.search('zzz')).toEqual([]);
  });
});
