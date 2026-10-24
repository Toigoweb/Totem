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
    percorso: section ? section.percorso : '',
    utente: section ? section.utente : '',
    password: '',
    sottocartelle: section ? section.sottocartelle : true,
  }));
  const [test, setTest] = useState({ status: 'idle' });
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState(null);
  const set = (k) => (v) => setF((x) => ({ ...x, [k]: v }));

  const nameOk = f.nome.trim().length > 0 && [...f.nome.trim()].length <= 30;
  const pathOk = validPath(f.percorso);
  const userOk = validUser(f.utente);
  const fp = `${f.percorso.toLowerCase()}\u0000${f.utente}\u0000${f.password}\u0000${f.sottocartelle}`;
  const shareChanged = isNew || f.percorso !== section.percorso || f.utente !== section.utente || f.password !== '';
  const testedNow = test.status === 'ok' && test.fp === fp;
  const canSave = !readOnly && !saving && nameOk && pathOk && userOk && (!shareChanged || testedNow);

  const runTest = async () => {
    setTest({ status: 'testing', fp });
    try {
      const r = await maintApi('/api/maint/test', {
        method: 'POST', timeoutMs: 60000,
        body: { sectionId: section ? section.id : '', percorso: f.percorso, utente: f.utente, password: f.password, sottocartelle: f.sottocartelle },
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
    const body = { nome: f.nome.trim(), icona: f.icona, emergenza: f.emergenza, percorso: f.percorso, utente: f.utente,
      password: f.password, sottocartelle: f.sottocartelle, testToken: testedNow ? test.token : '' };
    try {
      const r = isNew
        ? await maintApi('/api/maint/sections', { method: 'POST', body })
        : await maintApi(`/api/maint/sections/${section.id}`, { method: 'PUT', body });
      if (r.ok) {
        onSaved(r.data.id);
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
          <label for="f-percorso">{S.fPath}</label>
          <input id="f-percorso" class={`input mono ${f.percorso && !pathOk ? 'err' : ''}`} value={f.percorso} placeholder="\\server\condivisione\cartella"
            onInput={(e) => set('percorso')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
          {!f.percorso ? <span class="aiuto">{S.fPathHelp}</span>
            : pathOk ? <span class="msg-ok"><Icon name="spunta" />{S.fPathOk}</span>
            : <span class="msg-err" data-testid="path-err"><Icon name="errore" />{S.fPathErr}</span>}
        </div>
        <div class="campo">
          <label for="f-utente">{S.fUser}</label>
          <input id="f-utente" class={`input mono ${!userOk ? 'err' : ''}`} value={f.utente} placeholder="DOMINIO\utente"
            onInput={(e) => set('utente')(e.currentTarget.value)} autocomplete="off" spellcheck={false} />
          {userOk ? <span class="aiuto">{S.fUserHelp}</span> : <span class="msg-err"><Icon name="errore" />{S.fUserErr}</span>}
        </div>
        <div class="campo">
          <label for="f-password">{S.fPassword}</label>
          <input id="f-password" class="input" type="password" value={f.password} autocomplete="new-password"
            placeholder={isNew ? '' : S.fPasswordKeep} onInput={(e) => set('password')(e.currentTarget.value)} />
          {!isNew && <span class="aiuto">{S.fPasswordKeep}</span>}
        </div>
        <div class="campo">
          <span class="lab">{S.fSubfolders}</span>
          <Toggle on={f.sottocartelle} onChange={set('sottocartelle')} id="f-sottocartelle" />
        </div>
        <div class="campo largo">
          <div class="azioni">
            <button type="button" class="btn" data-testid="test-conn" disabled={!pathOk || !userOk || test.status === 'testing' || readOnly} onClick={runTest}>
              <Icon name="sync" />{S.testConn}
            </button>
            {test.status === 'testing' && <span class="esito-prova" role="status"><span class="spinner" />{S.testing}</span>}
            {test.status === 'ok' && test.fp === fp && <span class="esito-prova ok" role="status" data-testid="test-ok"><Icon name="spunta" />{S.testOk(test.count)}</span>}
            {test.status === 'err' && test.fp === fp && (
              <span class="esito-prova ko" role="status" data-testid="test-err">
                <Icon name={{ auth: 'lucchetto', not_found: 'cartella', unreachable: 'nuvolaOff', timeout: 'orologio', network: 'nuvolaOff' }[test.code] || 'errore'} />
                {S.testErr[test.code] || S.testErr.io}
              </span>
            )}
          </div>
        </div>
      </div>
      {err && <div class="banner rosso" style={{ marginTop: '16px' }}><Icon name="errore" /><div>{err}</div></div>}
      <div class="azioni" style={{ marginTop: '20px' }}>
        <button class="btn primario" data-testid="save" disabled={!canSave} onClick={save}><Icon name="spunta" />{submitLabel}</button>
        {onCancel && <button class="btn" onClick={onCancel}>{cancelLabel}</button>}
      </div>
    </div>
  );
}
