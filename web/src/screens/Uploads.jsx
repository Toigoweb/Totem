import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, go, exitMaintenance, maintApi, setToast } from '../app.js';
import { fmtBytes, fmtWhen } from '../format.js';
import { syncSoon, useServerOnline } from './Maint.jsx';

const exit = () => {
  exitMaintenance();
  go('home');
};

const FOLDER_BAD = /[/\\<>:"|?*\u0000-\u001f]/;
export const validFolder = (f) => f === '' || (!FOLDER_BAD.test(f) && !f.startsWith('.') && [...f].length <= 40);

/** Invia un PDF con XMLHttpRequest, per avere l'avanzamento. Risolve con { status, data }. */
function sendFile(url, file, token, onProgress) {
  return new Promise((resolve, reject) => {
    const x = new XMLHttpRequest();
    x.open('PUT', url);
    x.setRequestHeader('Content-Type', 'application/pdf');
    x.setRequestHeader('X-Maint-Token', token);
    x.upload.onprogress = (e) => e.lengthComputable && onProgress(Math.round((e.loaded / e.total) * 100));
    x.onload = () => {
      let data = {};
      try {
        data = JSON.parse(x.responseText) || {};
      } catch {
        data = {};
      }
      resolve({ status: x.status, data });
    };
    x.onerror = () => reject(new Error('rete'));
    x.send(file);
  });
}

/** Caricamento diretto dei PDF di una sezione di tipo "upload". */
export function Uploads({ id }) {
  const [sec, setSec] = useState(null);
  const [files, setFiles] = useState(null);
  const [maxBytes, setMaxBytes] = useState(100 * 1024 * 1024);
  const [folder, setFolder] = useState('');
  const [busy, setBusy] = useState(null); // { name, pct }
  const [errors, setErrors] = useState([]);
  const [confirm, setConfirm] = useState(null);
  const [online, setOnline] = useServerOnline();

  const load = async () => {
    try {
      const [a, b] = await Promise.all([maintApi('/api/maint/sections'), maintApi(`/api/maint/sections/${id}/files`)]);
      setSec((a.data.sections || []).find((x) => x.id === id) || null);
      if (b.ok) {
        setFiles(b.data.files || []);
        if (b.data.maxBytes) setMaxBytes(b.data.maxBytes);
      } else setFiles([]);
      setOnline(true);
    } catch {
      setOnline(false);
    }
  };
  useEffect(() => {
    load();
  }, [id]);

  const mb = Math.round(maxBytes / (1024 * 1024));
  const errMsg = (code) => (code === 'too_large' ? S.uploadErr.too_large(mb) : S.uploadErr[code] || S.uploadErr.io);

  const upload = async (list) => {
    const m = app.get().maint;
    if (!m || !list.length || !validFolder(folder.trim())) return;
    const errs = [];
    let ok = 0;
    for (const file of list) {
      if (!/\.pdf$/i.test(file.name)) {
        errs.push({ name: file.name, msg: S.uploadErr.invalid });
        continue;
      }
      if (file.size > maxBytes) {
        errs.push({ name: file.name, msg: errMsg('too_large') });
        continue;
      }
      setBusy({ name: file.name, pct: 0 });
      const q = new URLSearchParams({ nome: file.name });
      if (folder.trim()) q.set('cartella', folder.trim());
      try {
        const r = await sendFile(`/api/maint/sections/${id}/files?${q}`, file, m.token, (pct) => setBusy({ name: file.name, pct }));
        if (r.status === 201) ok++;
        else if (r.status === 401 && r.data.error === 'maintenance') {
          app.set({ maint: null });
          go('home');
          return;
        } else errs.push({ name: file.name, msg: errMsg(r.data.error) });
      } catch {
        errs.push({ name: file.name, msg: S.uploadErr.network });
        setOnline(false);
        break;
      }
    }
    setBusy(null);
    setErrors(errs);
    if (ok) {
      setToast(S.uploadDone(ok));
      syncSoon();
    }
    load();
  };

  const del = async (f) => {
    setConfirm(null);
    try {
      const r = await maintApi(`/api/maint/sections/${id}/files?path=${encodeURIComponent(f.path)}`, { method: 'DELETE' });
      if (r.ok) syncSoon();
      load();
    } catch {
      setOnline(false);
    }
  };

  const ro = !online || !!busy;
  const folderOk = validFolder(folder.trim());
  return (
    <div class="screen" data-testid="uploads">
      <TopBar left="back" onBack={() => go('maint')} title={S.uploadTitle(sec ? sec.nome : '…')} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">
        {!online && <div class="banner rosso"><Icon name="nuvolaOff" /><div>{S.maintOffline}</div></div>}
        <div class={`form ${!online ? 'disattivo' : ''}`}>
          <div class="campo">
            <label for="u-cartella">{S.uploadFolder}</label>
            <input id="u-cartella" class={`input ${!folderOk ? 'err' : ''}`} value={folder} maxLength={40} autocomplete="off"
              onInput={(e) => setFolder(e.currentTarget.value)} />
            {folderOk ? <span class="aiuto">{S.uploadFolderHelp}</span> : <span class="msg-err"><Icon name="errore" />{S.uploadFolderErr}</span>}
          </div>
          <div class="campo largo">
            <label class={`btn primario ${ro || !folderOk ? 'disattivo' : ''}`} style={{ alignSelf: 'flex-start', position: 'relative' }}>
              <Icon name="carica" />{S.uploadPick}
              <input type="file" accept="application/pdf,.pdf" multiple data-testid="upload-input" disabled={ro || !folderOk}
                style={{ position: 'absolute', width: '1px', height: '1px', opacity: 0, overflow: 'hidden' }}
                onChange={(e) => {
                  const list = [...e.currentTarget.files];
                  e.currentTarget.value = '';
                  upload(list);
                }} />
            </label>
            <span class="aiuto">{S.uploadHelp(mb)}</span>
          </div>
        </div>
        {busy && (
          <div role="status" data-testid="upload-progress" style={{ margin: '16px 0' }}>
            <p>{S.uploading(busy.name, busy.pct)}</p>
            <div class="progress"><div style={{ width: `${busy.pct}%` }} /></div>
          </div>
        )}
        {errors.map((e) => (
          <div class="banner rosso" key={e.name} data-testid="upload-err" style={{ marginTop: '12px' }}><Icon name="errore" /><div><strong>{e.name}</strong>: {e.msg}</div></div>
        ))}
        <div style={{ marginTop: '20px' }}>
          {files === null ? <p>{S.maintLoading}</p> : files.length === 0 ? <p data-testid="uploads-empty">{S.uploadEmpty}</p> : (
            files.slice().sort((a, b) => a.path.localeCompare(b.path, 'it')).map((f) => (
              <div class="sez-riga" key={f.path} data-testid="upload-row">
                <Icon name="pdf" />
                <div style={{ minWidth: 0 }}>
                  <div class="n">{f.path.split('/').pop()}{f.path.includes('/') && <span class="badge">{f.path.split('/')[0]}</span>}</div>
                  <div class="p">{fmtBytes(f.size)} · {fmtWhen(new Date(f.mtime))}</div>
                </div>
                <div class="b">
                  <button class="btn" disabled={ro} aria-label={`${S.del} ${f.path}`} onClick={() => setConfirm(f)}><Icon name="cestino" />{S.del}</button>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
      {confirm && (
        <div class="velo">
          <div class="dialogo" role="dialog" aria-modal="true" aria-labelledby="dlg-u">
            <h2 id="dlg-u">{S.uploadDelQ(confirm.path.split('/').pop())}</h2>
            <p>{S.uploadDelText}</p>
            <div class="azioni">
              <button class="btn rosso" data-testid="confirm-delete" onClick={() => del(confirm)}><Icon name="cestino" />{S.del}</button>
              <button class="btn" onClick={() => setConfirm(null)}>{S.cancel}</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
