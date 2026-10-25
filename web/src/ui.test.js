import { describe, it, expect } from 'vitest';
import { pillState, recentDocs, activeNotices } from './app.js';
import { screensaverMode } from './kiosk/Screensaver.jsx';
import { inWindow, shouldApply } from './kiosk/updates.js';
import { validPath, validUser, checkSource } from './screens/SectionForm.jsx';
import { validFolder } from './screens/Uploads.jsx';
import { filterEvents, countByType, daysWithoutInjury } from './heatmap.js';
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

describe('avvisi e salvaschermo', () => {
  it('avvisi attivi oggi, date incluse', () => {
    const ns = [{ id: 'a', from: '2026-10-24', to: '2026-10-24' }, { id: 'b', to: '2026-10-23' }, { id: 'c', from: '2026-10-25' }, { id: 'd' }];
    expect(activeNotices(ns, new Date('2026-10-24T10:00:00')).map((n) => n.id)).toEqual(['a', 'd']);
  });
  it('salvaschermo: solo totem, dopo N minuti, nero di notte, mai in manutenzione', () => {
    const t = new Date('2026-10-24T10:00:00');
    const base = { role: 'totem', maint: null, route: { name: 'home' }, lastInteraction: t.getTime() - 6 * 60000,
      extras: { screensaver: { enabled: true, afterMinutes: 5, night: true, nightHours: '20:00-06:00' } } };
    expect(screensaverMode(base, t)).toBe('info');
    expect(screensaverMode({ ...base, lastInteraction: t.getTime() - 60000 }, t)).toBe(null);
    expect(screensaverMode({ ...base, role: 'postazione' }, t)).toBe(null);
    expect(screensaverMode({ ...base, maint: { token: 'x' } }, t)).toBe(null);
    expect(screensaverMode({ ...base, route: { name: 'wizard' } }, t)).toBe(null);
    expect(screensaverMode(base, new Date('2026-10-24T22:00:00'))).toBe('nero');
    const off = { ...base, extras: { screensaver: { ...base.extras.screensaver, enabled: false } } };
    expect(screensaverMode(off, t)).toBe(null);
    expect(screensaverMode(off, new Date('2026-10-24T22:00:00'))).toBe('nero');
  });
});

describe('origini dei PDF', () => {
  const base = { percorso: '', utente: '', tenant: '', password: '' };
  it('OneDrive: link SharePoint, tenant e client ID', () => {
    const ok = { ...base, tipo: 'onedrive', percorso: 'https://azienda.sharepoint.com/:f:/s/HSE/Eabc', tenant: 'azienda.onmicrosoft.com', utente: '1b2c3d4e-0000-1111-2222-333344445555' };
    expect(checkSource(ok).ok).toBe(true);
    expect(checkSource({ ...ok, percorso: 'https://example.com/x' }).path).toBe(false);
    expect(checkSource({ ...ok, percorso: 'http://azienda.sharepoint.com/x' }).path).toBe(false);
    expect(checkSource({ ...ok, tenant: 'azienda' }).tenant).toBe(false);
    expect(checkSource({ ...ok, utente: 'mario' }).user).toBe(false);
  });
  it('WebDAV: http(s) senza credenziali nell\'URL', () => {
    expect(checkSource({ ...base, tipo: 'webdav', percorso: 'https://cloud.azienda.it/remote.php/dav/files/totem/SDS' }).ok).toBe(true);
    expect(checkSource({ ...base, tipo: 'webdav', percorso: 'https://u:p@cloud.azienda.it/dav' }).ok).toBe(false);
    expect(checkSource({ ...base, tipo: 'webdav', percorso: 'ftp://nas/dav' }).ok).toBe(false);
  });
  it('caricamento diretto: niente da controllare; sottocartella semplice', () => {
    expect(checkSource({ ...base, tipo: 'upload' }).ok).toBe(true);
    expect(validFolder('Edificio A')).toBe(true);
    expect(validFolder('a/b')).toBe(false);
    expect(validFolder('.x')).toBe(false);
    expect(validFolder('x'.repeat(41))).toBe(false);
  });
  it('SMB invariato', () => {
    expect(checkSource({ ...base, tipo: 'smb', percorso: '\\\\srv\\sds', utente: 'AZ\\totem' }).ok).toBe(true);
  });
});

describe('mappa incidenti', () => {
  const ev = [
    { map: 'm1', x: 0.1, y: 0.1, date: '2026-10-20', type: 'infortunio' },
    { map: 'm1', x: 0.2, y: 0.2, date: '2026-06-01', type: 'quasi' },
    { map: 'm1', x: 0.3, y: 0.3, date: '2025-01-15', type: 'danno' },
    { map: 'm2', x: 0.3, y: 0.3, date: '2026-10-01', type: 'medicazione' },
    { map: 'm1', x: 0.3, y: 0.3, date: '2026-10-30', type: 'quasi' }, // nel futuro (orologio diverso): escluso
  ];
  const today = '2026-10-25';
  it('filtra per planimetria, periodo e tipo', () => {
    expect(filterEvents(ev, { map: 'm1', period: '3m', today })).toHaveLength(1);
    expect(filterEvents(ev, { map: 'm1', period: '12m', today })).toHaveLength(2);
    expect(filterEvents(ev, { map: 'm1', period: 'all', today })).toHaveLength(3);
    expect(filterEvents(ev, { map: 'm1', period: 'all', types: ['quasi'], today })).toHaveLength(1);
    expect(countByType(filterEvents(ev, { map: 'm1', period: 'all', today }))).toEqual({ infortunio: 1, medicazione: 0, quasi: 1, danno: 1 });
  });
  it('giorni senza infortuni', () => {
    expect(daysWithoutInjury('2026-10-20', '2026-01-01', today)).toBe(5);
    expect(daysWithoutInjury('', '2026-10-01', today)).toBe(24);
    expect(daysWithoutInjury('2025-05-01', '2026-10-01', today)).toBe(24); // infortunio prima dell'inizio
    expect(daysWithoutInjury('2026-10-20', '', today)).toBe(null);
  });
});
