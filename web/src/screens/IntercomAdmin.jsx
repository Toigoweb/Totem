// Manutenzione → Interfono: interruttore generale, dispositivi, ruoli e abbinamento dei
// telefoni con codice e QR.
import { useEffect, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar, MaintBand, Toggle } from '../components/TopBar.jsx';
import { Qr } from '../components/Qr.jsx';
import { Chips } from './Extras.jsx';
import { S } from '../strings.js';
import { go, exitMaintenance, maintApi, setToast, getManager } from '../app.js';
import { mmss } from '../format.js';

const exit = () => {
  exitMaintenance();
  go('home');
};

/** Il manifest del dispositivo cambia (campo intercom): lo si riprende subito. */
function syncSoon() {
  const m = getManager();
  if (m) setTimeout(() => m.syncNow(), 1000);
}

export function IntercomAdmin() {
  const [data, setData] = useState(null);
  const [groups, setGroups] = useState([]);
  const [channels, setChannels] = useState([]);
  const [err, setErr] = useState(null);
  const [phoneName, setPhoneName] = useState('');
  const [code, setCode] = useState(null); // { code, url, until }
  const [, tick] = useState(0);

  const load = async () => {
    try {
      const r = await maintApi('/api/maint/intercom');
      if (r.ok) {
        setData(r.data);
        setGroups(r.data.gruppi.map((g) => ({ ...g })));
        setChannels((r.data.canali || []).map((c) => ({ ...c })));
      }
    } catch {
      setErr(S.maintOffline);
    }
  };
  useEffect(() => {
    load();
  }, []);
  useEffect(() => {
    if (!code) return;
    const t = setInterval(() => {
      if (Date.now() > code.until) setCode(null);
      tick((x) => x + 1);
    }, 1000);
    return () => clearInterval(t);
  }, [code]);

  const saveAll = async (attivo, gs = groups, chs = channels) => {
    setErr(null);
    try {
      const r = await maintApi('/api/maint/intercom', { method: 'PUT', body: { attivo, gruppi: gs, canali: chs } });
      if (!r.ok) {
        setErr(S.icAdminErr[r.data.field] || S.saveErr);
        return;
      }
      setData(r.data);
      setGroups(r.data.gruppi.map((g) => ({ ...g })));
      setChannels(r.data.canali.map((c) => ({ ...c })));
      setToast(S.icAdminSaved);
      syncSoon();
    } catch {
      setErr(S.maintOffline);
    }
  };
  const saveDevice = async (d, patch) => {
    const body = { attivo: d.attivo, numero: d.numero, ...patch };
    try {
      const r = await maintApi(`/api/maint/devices/${d.id}/intercom`, { method: 'PUT', body });
      if (!r.ok) setErr(S.icAdminErr.numero);
      else syncSoon();
    } catch {
      setErr(S.maintOffline);
    }
    load();
  };
  const pairPhone = async () => {
    try {
      const r = await maintApi('/api/maint/intercom/phone-code', { method: 'POST', body: { nome: phoneName } });
      if (r.ok) setCode({ code: r.data.code, url: location.origin + r.data.path, until: Date.now() + r.data.expiresInSec * 1000 });
      else setErr(S.genericErr);
    } catch {
      setErr(S.maintOffline);
    }
  };

  const updCh = (i, patch) => setChannels(channels.map((c, j) => (j === i ? { ...c, ...patch } : c)));
  const upd = (i, patch) => setGroups(groups.map((g, j) => (j === i ? { ...g, ...patch } : g)));
  const members = data ? data.devices.filter((d) => d.attivo).map((d) => ({ id: d.id, nome: d.nome })) : [];

  return (
    <div class="screen" data-testid="intercom-admin">
      <TopBar left="back" onBack={() => go('maint')} title={S.icAdmin} right={null} />
      <MaintBand onExit={exit} />
      <div class="body">
        {err && <div class="banner rosso" role="alert"><Icon name="errore" /><div>{err}</div></div>}
        {data && (
          <>
            <div class="campo">
              <span class="lab">{S.icAdminOn}</span>
              <Toggle id="ic-on" on={data.attivo} onChange={(v) => saveAll(v)} help={S.icAdminOnHelp} />
            </div>

            {data.attivo && (
              <>
                {!data.push && <div class="banner"><Icon name="info" /><div>{S.icAdminNoPush}</div></div>}

                <h2 class="blocco-tit">{S.icAdminDevices}</h2>
                <p class="piccolo">{S.icAdminDevicesHelp}</p>
                {data.devices.map((d) => (
                  <div class="riga-ed" key={d.id} data-testid="ic-admin-device">
                    <div class="form">
                      <div class="campo">
                        <span class="lab">{d.nome}<small> · {S.roleNames[d.ruolo] || d.ruolo}{d.repartoNome ? ` · ${d.repartoNome}` : ''}</small></span>
                        <Toggle on={d.attivo} onChange={(v) => saveDevice(d, { attivo: v })} label={`${S.icTitle} ${d.nome}`} />
                      </div>
                      {d.attivo && (
                        <div class="campo">
                          <label for={`icn-${d.id}`}>{S.icAdminNumber}</label>
                          <input id={`icn-${d.id}`} class="input mono" maxLength={30} inputMode="tel" value={d.numero}
                            onChange={(e) => saveDevice(d, { numero: e.currentTarget.value })} />
                        </div>
                      )}
                      {d.attivo && (
                        <p class="piccolo">
                          {d.online ? <><Icon name="spunta" />{S.icAdminOnline}</> : <><Icon name="x" />{S.icUnreach}</>}
                          {d.ruolo === 'telefono' && d.push && <> · <Icon name="campana" />{S.icAdminPush}</>}
                        </p>
                      )}
                    </div>
                  </div>
                ))}

                <h2 class="blocco-tit">{S.icAdminGroups}</h2>
                <p class="piccolo">{S.icAdminGroupsHelp}</p>
                {groups.map((g, i) => (
                  <div class="riga-ed" key={g.id || `n${i}`} data-testid="ic-admin-group">
                    <div class="form">
                      <div class="campo largo">
                        <label for={`icg-${i}`}>{S.icAdminGroupName}</label>
                        <input id={`icg-${i}`} class="input" maxLength={40} value={g.nome} onInput={(e) => upd(i, { nome: e.currentTarget.value })} />
                      </div>
                      <div class="campo">
                        <label for={`icgn-${i}`}>{S.icAdminNumber}</label>
                        <input id={`icgn-${i}`} class="input mono" maxLength={30} inputMode="tel" value={g.numero || ''} onInput={(e) => upd(i, { numero: e.currentTarget.value })} />
                      </div>
                      {members.length ? (
                        <Chips label={S.icAdminMembers} options={members} value={g.membri} onChange={(v) => upd(i, { membri: v })} />
                      ) : <p class="piccolo">{S.icAdminNoMembers}</p>}
                    </div>
                    <button class="btn" aria-label={S.remove} onClick={() => setGroups(groups.filter((_, j) => j !== i))}><Icon name="cestino" /></button>
                  </div>
                ))}
                <div class="azioni">
                  <button class="btn" data-testid="ic-group-add" disabled={groups.length >= 20} onClick={() => setGroups([...groups, { id: '', nome: '', numero: '', membri: [] }])}>
                    <Icon name="piu" />{S.icAdminGroupAdd}
                  </button>
                  <button class="btn primario" data-testid="ic-save" onClick={() => saveAll(data.attivo)}><Icon name="spunta" />{S.save2}</button>
                </div>

                <h2 class="blocco-tit">{S.icAdminChannels}</h2>
                <p class="piccolo">{S.icAdminChannelsHelp}</p>
                {channels.map((c, i) => (
                  <div class="riga-ed" key={c.id || `c${i}`} data-testid="ic-admin-channel">
                    <div class="form">
                      <div class="campo largo">
                        <label for={`icc-${i}`}>{S.icAdminChannelName}</label>
                        <input id={`icc-${i}`} class="input" maxLength={40} value={c.nome} onInput={(e) => updCh(i, { nome: e.currentTarget.value })} />
                      </div>
                      {members.length ? (
                        <Chips label={S.icAdminMembers} options={members} value={c.membri} onChange={(v) => updCh(i, { membri: v })} />
                      ) : <p class="piccolo">{S.icAdminNoMembers}</p>}
                    </div>
                    <button class="btn" aria-label={S.remove} onClick={() => setChannels(channels.filter((_, j) => j !== i))}><Icon name="cestino" /></button>
                  </div>
                ))}
                <div class="azioni">
                  <button class="btn" data-testid="ic-channel-add" disabled={channels.length >= 20} onClick={() => setChannels([...channels, { id: '', nome: '', membri: [] }])}>
                    <Icon name="piu" />{S.icAdminChannelAdd}
                  </button>
                  <button class="btn primario" data-testid="ic-save-channels" onClick={() => saveAll(data.attivo)}><Icon name="spunta" />{S.save2}</button>
                </div>

                <h2 class="blocco-tit">{S.icAdminPhone}</h2>
                <p class="piccolo">{S.icAdminPhoneHelp}</p>
                {!code ? (
                  <div class="form">
                    <div class="campo largo">
                      <label for="ic-pn">{S.icAdminPhoneName}</label>
                      <input id="ic-pn" class="input" maxLength={40} value={phoneName} onInput={(e) => setPhoneName(e.currentTarget.value)} />
                      <p class="aiuto">{S.icAdminPhoneNameHelp}</p>
                    </div>
                    <div class="azioni">
                      <button class="btn primario" data-testid="ic-phone-code" onClick={pairPhone}><Icon name="telefono" />{S.icAdminPhone}</button>
                    </div>
                  </div>
                ) : (
                  <div class="ic-codice" data-testid="ic-phone-pair">
                    <Qr text={code.url} label={S.icAdminPhone} />
                    <div>
                      <p>{S.icAdminPhoneCode}</p>
                      <p class="mono url">{location.origin}</p>
                      <p class="codice-grande" data-testid="ic-code">{code.code.slice(0, 4)}-{code.code.slice(4)}</p>
                      <p class="piccolo"><Icon name="orologio" />{S.icAdminPhoneExpires(mmss((code.until - Date.now()) / 1000))}</p>
                      <button class="btn" onClick={() => { setCode(null); load(); }}>{S.close}</button>
                    </div>
                  </div>
                )}
              </>
            )}
          </>
        )}
      </div>
    </div>
  );
}
