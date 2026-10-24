import { useState } from 'preact/hooks';
import { Icon, SECTION_ICONS } from '../components/Icon.jsx';
import { Toggle } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { maintApi } from '../app.js';
import { NetworkError } from '../api.js';

// Stesse regole del server (internal/config): \\server\condivisione[\cartella…]
const UNC = /^\\\\[A-Za-z0-9._-]+\\[^\\/:*?"<>|]+(\\[^\\/:*?"<>|]+)*\\?$/;
const USER = /^([A-Za-z0-9._-]+\\)?[A-Za-z0-9._@$-]+$/;

export function validPath(p) {
  if (!UNC.test(p)) return false;
  return p.slice(2).split('\\').every((x) => x !== '.' && x !== '..' && !/[\x00-\x1f]/.test(x));
}
export const validUser = (u) => u === '' || USER.test(u);

// Altre origini: stesse regole di internal/config
const GUID = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
const DOMAIN = /^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$/;
function parseURL(u) {
  try {
    return new URL(u);
  } catch {
    return null;
  }
}
export function validDavURL(u) {
  const p = parseURL(u);
  return !!p && (p.protocol === 'https:' || p.protocol === 'http:') && !!p.host && !p.username && !p.password && u.length <= 500;
}
export function validOneDriveLink(u) {
  const p = parseURL(u);
  if (!p || p.protocol !== 'https:' || p.username || u.length > 1000) return false;
  const h = p.hostname.toLowerCase();
  return h.endsWith('.sharepoint.com') || h === 'onedrive.live.com' || h === '1drv.ms' || h.endsWith('.sharepoint.us');
}
export const validTenant = (t) => GUID.test(t) || (DOMAIN.test(t) && t.length <= 253);
export const validClientId = (c) => GUID.test(c);

export const SOURCE_TYPES = ['smb', 'onedrive', 'webdav', 'upload'];
const SOURCE_ICON = { smb: 'rete', onedrive: 'nuvola', webdav: 'nuvola', upload: 'carica' };

/** Controllo dei campi dell'origine: restituisce { ok, path, user, tenant } (true = valido). */
export function checkSource(f) {
  switch (f.tipo) {
    case 'onedrive': {
      const path = validOneDriveLink(f.percorso);
      const user = validClientId(f.utente);
      const tenant = validTenant(f.tenant);
      return { ok: path && user && tenant, path, user, tenant };
    }
    case 'webdav': {
      const path = validDavURL(f.percorso);
      const user = f.utente.length <= 100;
      return { ok: path && user, path, user, tenant: true };
    }
    case 'upload':
      return { ok: true, path: true, user: true, tenant: true };
    default: {
      const path = validPath(f.percorso);
      const user = validUser(f.utente);
      return { ok: path && user, path, user, tenant: true };
    }
  }
}

/**
 * Form unico per creare o modificare una sezione (schermata 7 e passo 3 della procedura).
 * section: sezione esistente (dal server) oppure null.
 */
export function SectionForm({ section, onSaved, onCancel, submitLabel = S.save, cancelLabel = S.cancel, readOnly = false }) {
  const isNew = !section;
  const [f, setF] = useState(() => ({
    nome: section ? section.nome : '',
    icona: section ? section.icona : 'documento',
    emergenza: section ? section.emergenza : false,
    tipo: (section && section.tipo) || 'smb',
    percorso: section ? section.percorso : '',
    utente: section ? section.utente : '',
    tenant: (section && section.tenant) || '',
    password: '',
    sottocartelle: section ? section.sottocartelle : true,
  }));
  const [test, setTest] = useState({ status: 'idle' });
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState(null);
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));
  // cambiando tipo i campi dell'origine ripartono vuoti (tranne tornando al tipo salvato)
  const setTipo = (tipo) => setF((x) => {
    if (x.tipo === tipo) return x;
    const back = section && (section.tipo || 'smb') === tipo;
    return { ...x, tipo, percorso: back ? section.percorso : '', utente: back ? section.utente : '', tenant: back ? section.tenant || '' : '', password: '' };
  });

  const isUpload = f.tipo === 'upload';
  const nameOk = f.nome.trim().length > 0 && [...f.nome.trim()].length <= 30;
  const chk = checkSource(f);
  const fp = [f.tipo, f.percorso.toLowerCase(), f.utente, f.password, f.tenant.toLowerCase(), f.sottocartelle].join('\u0000');
  const savedTipo = section ? section.tipo || 'smb' : null;
  const shareChanged = isNew || f.tipo !== savedTipo || f.percorso !== section.percorso || f.utente !== section.utente ||
    f.tenant !== (section.tenant || '') || f.password !== '';
  const testedNow = test.status === 'ok' && test.fp === fp;
  const canSave = !readOnly && !saving && nameOk && chk.ok && (isUpload || !shareChanged || testedNow);

  const runTest = async () => {
    setTest({ status: 'testing', fp });
    try {
      const r = await maintApi('/api/maint/test', {
        method: 'POST', timeoutMs: 90000,
        body: { sectionId: section ? section.id : '', tipo: f.tipo, percorso: f.percorso, utente: f.utente, tenant: f.tenant, password: f.password, sottocartelle: f.sottocartelle },
      });
      if (r.data.ok) setTest({ status: 'ok', fp, count: r.data.pdfCount, token: r.data.testToken });
      else setTest({ status: 'err', fp, code: r.data.code || 'io' });
    } catch (e) {
      setTest({ status: 'err', fp, code: e instanceof NetworkError ? 'network' : 'io' });
    }
  };

  const save = async () => {
    if (!canSave) return;
    setSaving(true);
    setErr(null);
    const body = { nome: f.nome.trim(), icona: f.icona, emergenza: f.emergenza, tipo: f.tipo, percorso: isUpload ? '' : f.percorso.trim(),
      utente: isUpload ? '' : f.utente.trim(), tenant: f.tipo === 'onedrive' ? f.tenant.trim() : '',
      password: isUpload ? '' : f.password, sottocartelle: isUpload ? true : f.sottocartelle, testToken: testedNow ? test.token : '' };
    try {
      const r = isNew
        ? await maintApi('/api/maint/sections', { method: 'POST', body })
        : await maintApi(`/api/maint/sections/${section.id}`, { method: 'PUT', body });
      if (r.ok) {
        onSaved(r.data.id, f.tipo);
        return;
      }
      if (r.status === 409 && r.data.error === 'test_required') setErr(S.testNeeded);
      else if (r.status === 409 && r.data.error === 'too_many') setErr(S.maxSections(8));
      else setErr(S.saveErr);
    } catch {
      setErr(S.saveErr);
    }
    setSaving(false);
  };

  const errText = (code) => (f.tipo === 'onedrive' && S.testErrOnedrive[code]) || (f.tipo === 'webdav' && S.testErrWebdav[code]) || S.testErr[code] || S.testErr.io;
  const fieldMsg = (val, ok, help, bad, okMsg) => (!val ? <span class="aiuto">{help}</span>
    : ok ? (okMsg ? <span class="msg-ok"><Icon name="spunta" />{okMsg}</span> : <span class="aiuto">{help}</span>)
    : <span class="msg-err" data-testid="path-err"><Icon name="errore" />{bad}</span>);
  const secretField = (label) => (
    <div class="campo">
      <label for="f-password">{label}</label>
      <input id="f-password" class="input" type="password" value={f.password} autocomplete="new-password"
        placeholder={!isNew && f.tipo === savedTipo ? S.fPasswordKeep : ''} onInput={(e) => set('password')(e.currentTarget.value)} />
      {!isNew && f.tipo === savedTipo && <span class="aiuto">{S.fPasswordKeep}</span>}
    </div>
  );

  return (
    <div class={readOnly ? 'disattivo' : ''}>
      <div class="form" data-testid="section-form">
        <div class="campo">
          <label for="f-nome">{S.fName}</label>
          <input id="f-nome" class={`input ${f.nome && !nameOk ? 'err' : ''}`} maxLength={30} value={f.nome}
            onInput={(e) => set('nome')(e.currentTarget.value)} autocomplete="off" />
          {f.nome.trim() === '' && test.status !== 'idle' ? <span class="msg-err"><Icon name="errore" />{S.fNameRequired}</span>
            : <span class="aiuto">{S.fNameCount([...f.nome].length)}</span>}
        </div>
        <div class="campo">
          <span class="lab">{S.fEmergency}</span>
          <Toggle on={f.emergenza} onChange={set('emergenza')} help={S.fEmergencyHelp} id="f-emergenza" />
        </div>
        <div class="campo largo">
          <span class="lab">{S.fIcon}</span>
          <div class="icone10" role="group" aria-label={S.fIcon}>
            {SECTION_ICONS.map((ic) => (
              <button key={ic} type="button" aria-label={S.icons[ic]} aria-pressed={f.icona === ic ? 'true' : 'false'} onClick={() => set('icona')(ic)}>
                <Icon name={ic} />
                {f.icona === ic && <span class="spunta"><Icon name="check" /></span>}
              </button>
            ))}
          </div>
        </div>
        <div class="campo largo">
          <span class="lab">{S.fSource}</span>
          <div class="chips" role="group" aria-label={S.fSource} data-testid="source-type">
            {SOURCE_TYPES.map((t) => (
              <button key={t} type="button" class="chip" aria-pressed={f.tipo === t ? 'true' : 'false'} data-tipo={t} onClick={() => setTipo(t)}>
                <Icon name={SOURCE_ICON[t]} />{S.sources[t]}
              </button>
            ))}
          </div>
          <span class="aiuto">{S.sourcesHelp[f.tipo]}</span>
        </div>
        {f.tipo === 'smb' && <>
          <div class="campo largo">
            <label for="f-percorso">{S.fPath}</label>
            <input id="f-percorso" class={`input mono ${f.percorso && !chk.path ? 'err' : ''}`} value={f.percorso} placeholder="\\server\condivisione\cartella"
              onInput={(e) => set('percorso')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
            {fieldMsg(f.percorso, chk.path, S.fPathHelp, S.fPathErr, S.fPathOk)}
          </div>
          <div class="campo">
            <label for="f-utente">{S.fUser}</label>
            <input id="f-utente" class={`input mono ${!chk.user ? 'err' : ''}`} value={f.utente} placeholder="DOMINIO\utente"
              onInput={(e) => set('utente')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
            {chk.user ? <span class="aiuto">{S.fUserHelp}</span> : <span class="msg-err"><Icon name="errore" />{S.fUserErr}</span>}
          </div>
          {secretField(S.fPassword)}
        </>}
        {f.tipo === 'onedrive' && <>
          <div class="campo largo">
            <label for="f-percorso">{S.fOdLink}</label>
            <input id="f-percorso" class={`input mono ${f.percorso && !chk.path ? 'err' : ''}`} value={f.percorso} placeholder="https://azienda.sharepoint.com/:f:/s/…"
              onInput={(e) => set('percorso')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
            {fieldMsg(f.percorso, chk.path, S.fOdLinkHelp, S.fOdLinkErr)}
          </div>
          <div class="campo">
            <label for="f-tenant">{S.fTenant}</label>
            <input id="f-tenant" class={`input mono ${f.tenant && !chk.tenant ? 'err' : ''}`} value={f.tenant} placeholder="azienda.onmicrosoft.com"
              onInput={(e) => set('tenant')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
            {fieldMsg(f.tenant, chk.tenant, S.fTenantHelp, S.fTenantErr)}
          </div>
          <div class="campo">
            <label for="f-utente">{S.fClientId}</label>
            <input id="f-utente" class={`input mono ${f.utente && !chk.user ? 'err' : ''}`} value={f.utente} placeholder="00000000-0000-0000-0000-000000000000"
              onInput={(e) => set('utente')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
            {f.utente && !chk.user && <span class="msg-err"><Icon name="errore" />{S.fClientIdErr}</span>}
          </div>
          {secretField(S.fSecret)}
        </>}
        {f.tipo === 'webdav' && <>
          <div class="campo largo">
            <label for="f-percorso">{S.fDavUrl}</label>
            <input id="f-percorso" class={`input mono ${f.percorso && !chk.path ? 'err' : ''}`} value={f.percorso} placeholder="https://cloud.azienda.it/remote.php/dav/files/totem/SDS"
              onInput={(e) => set('percorso')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
            {fieldMsg(f.percorso, chk.path, S.fDavUrlHelp, S.fDavUrlErr)}
          </div>
          <div class="campo">
            <label for="f-utente">{S.fUser}</label>
            <input id="f-utente" class="input mono" value={f.utente} maxLength={100}
              onInput={(e) => set('utente')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
            <span class="aiuto">{S.fDavUserHelp}</span>
          </div>
          {secretField(S.fPassword)}
        </>}
        {!isUpload && (
          <div class="campo">
            <span class="lab">{S.fSubfolders}</span>
            <Toggle on={f.sottocartelle} onChange={set('sottocartelle')} id="f-sottocartelle" />
          </div>
        )}
        {isUpload ? (
          <div class="campo largo"><div class="banner info" data-testid="upload-info"><Icon name="info" /><div>{S.uploadAfterSave}</div></div></div>
        ) : <div class="campo largo">
          <div class="azioni">
            <button type="button" class="btn" data-testid="test-conn" disabled={!chk.ok || test.status === 'testing' || readOnly} onClick={runTest}>
              <Icon name="sync" />{S.testConn}
            </button>
            {test.status === 'testing' && <span class="esito-prova" role="status"><span class="spinner" />{S.testing}</span>}
            {test.status === 'ok' && test.fp === fp && <span class="esito-prova ok" role="status" data-testid="test-ok"><Icon name="spunta" />{S.testOk(test.count)}</span>}
            {test.status === 'err' && test.fp === fp && (
              <span class="esito-prova ko" role="status" data-testid="test-err">
                <Icon name={{ auth: 'lucchetto', not_found: 'cartella', unreachable: 'nuvolaOff', timeout: 'orologio', network: 'nuvolaOff' }[test.code] || 'errore'} />
                {errText(test.code)}
              </span>
            )}
          </div>
        </div>}
      </div>
      {err && <div class="banner rosso" style={{ marginTop: '16px' }}><Icon name="errore" /><div>{err}</div></div>}
      <div class="azioni" style={{ marginTop: '20px' }}>
        <button class="btn primario" data-testid="save" disabled={!canSave} onClick={save}><Icon name="spunta" />{submitLabel}</button>
        {onCancel && <button class="btn" onClick={onCancel}>{cancelLabel}</button>}
      </div>
    </div>
  );
}
