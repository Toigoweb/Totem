import { useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { Icon } from '../components/Icon.jsx';
import { TopBar } from '../components/TopBar.jsx';
import { S } from '../strings.js';
import { app, localDay } from '../app.js';
import { useStore } from '../store.js';
import { CACHE_NAME, mapURL } from '../sync/sync.js';
import { EVENT_TYPES, PERIODS, filterEvents, countByType, daysWithoutInjury, drawHeat } from '../heatmap.js';

const clamp = (v) => Math.min(1, Math.max(0, v));

/** Simbolo del tipo di evento: forma diversa per ogni tipo, non solo il colore. */
export function EvMark({ type }) {
  return <span class={`segno t-${type}`} aria-hidden="true" />;
}

/**
 * Planimetria con mappa di calore e punti. onPick (manutenzione) riceve il punto toccato
 * in coordinate 0..1; onMarker il punto esistente toccato.
 */
export function MapView({ map, src, heat = [], markers = [], onPick, onMarker, testid = 'map-view' }) {
  const canvas = useRef(null);
  const cw = Math.min(map.width, 1200);
  const ch = Math.max(1, Math.round((cw * map.height) / map.width));
  useEffect(() => {
    if (canvas.current) drawHeat(canvas.current, heat);
  }, [heat, cw, ch]);
  const pick = (e) => {
    if (!onPick) return;
    const r = e.currentTarget.getBoundingClientRect();
    onPick({ x: clamp((e.clientX - r.left) / r.width), y: clamp((e.clientY - r.top) / r.height) });
  };
  return (
    <div class={`mappa ${onPick ? 'scegli' : ''}`} data-testid={testid} onClick={pick}
      style={{ aspectRatio: `${map.width} / ${map.height}`, width: `min(100%, calc(68vh * ${map.width / map.height}))` }}>
      {src ? <img src={src} alt={map.name} draggable={false} /> : <div class="attesa"><span class="spinner" />{S.mapLoading}</div>}
      <canvas ref={canvas} width={cw} height={ch} aria-hidden="true" />
      {markers.map((m, i) => (onMarker ? (
        <button key={m.id || i} type="button" class={`punto t-${m.type}`} style={{ left: `${m.x * 100}%`, top: `${m.y * 100}%` }}
          aria-label={`${S.evType1[m.type]} ${m.date}`} data-testid="marker"
          onClick={(ev) => { ev.stopPropagation(); onMarker(m); }} />
      ) : (
        <span key={m.id || i} class={`punto t-${m.type}`} style={{ left: `${m.x * 100}%`, top: `${m.y * 100}%` }} data-testid="marker" />
      )))}
    </div>
  );
}

/** Immagine della planimetria dalla cache (funziona offline). */
function useCachedImage(sha) {
  const [url, setUrl] = useState(null);
  useEffect(() => {
    let alive = true;
    let u = null;
    setUrl(null);
    caches.open(CACHE_NAME).then((c) => c.match(mapURL(sha))).then(async (res) => {
      if (!res || !alive) return;
      u = URL.createObjectURL(await res.blob());
      setUrl(u);
    }).catch(() => {});
    return () => {
      alive = false;
      if (u) URL.revokeObjectURL(u);
    };
  }, [sha]);
  return url;
}

/** Giorni senza infortuni dai dati della sync (null = contatore non attivo). */
export function useDaysWithoutInjury() {
  const s = useStore(app);
  const sf = s.extras.safety;
  return sf ? daysWithoutInjury(sf.lastInjury, sf.since, localDay()) : null;
}

export function SafetyMap() {
  const s = useStore(app);
  const sf = s.extras.safety;
  const maps = (sf && sf.maps) || [];
  const [mapId, setMapId] = useState(maps.length ? maps[0].id : null);
  const [period, setPeriod] = useState('12m');
  const [types, setTypes] = useState(EVENT_TYPES);
  const [points, setPoints] = useState(false);
  const map = maps.find((m) => m.id === mapId) || maps[0];
  const today = localDay();
  const events = useMemo(() => (map ? filterEvents(sf.events, { map: map.id, period, types, today }) : []), [sf, map && map.id, period, types, today]);
  const all = useMemo(() => (map ? filterEvents(sf.events, { map: map.id, period, today }) : []), [sf, map && map.id, period, today]);
  const counts = countByType(all);
  const src = useCachedImage(map ? map.sha256 : '');
  const days = sf ? daysWithoutInjury(sf.lastInjury, sf.since, today) : null;
  const toggle = (t) => setTypes((x) => (x.includes(t) ? (x.length > 1 ? x.filter((y) => y !== t) : x) : [...x, t]));

  return (
    <div class="screen" data-testid="safety">
      <TopBar left="back" title={S.safetyTitle} />
      <div class="body">
        {!map ? <div class="vuoto"><div class="c"><Icon name="mappa" class="grande" /><h2>{S.safetyEmpty}</h2></div></div> : (
          <div class="sic">
            <div class="sic-mappa">
              {maps.length > 1 && (
                <div class="chips" role="group" aria-label={S.mapsTitle}>
                  {maps.map((m) => (
                    <button key={m.id} class="chip" aria-pressed={m.id === map.id ? 'true' : 'false'} onClick={() => setMapId(m.id)}>{m.name}</button>
                  ))}
                </div>
              )}
              <MapView map={map} src={src} heat={events} markers={points ? events : []} />
              <div class="legenda" aria-hidden="true"><span>{S.heatLess}</span><span class="scala" /><span>{S.heatMore}</span></div>
            </div>
            <div class="sic-lato">
              {days !== null && (
                <div class="contatore" data-testid="days-counter">
                  <span class="n">{days}</span>
                  <span class="l">{S.daysNoInjuryShort}</span>
                </div>
              )}
              <div class="seg" role="group" aria-label={S.periodLabel}>
                {Object.keys(PERIODS).map((p) => (
                  <button key={p} aria-pressed={p === period ? 'true' : 'false'} onClick={() => setPeriod(p)}>{S.periods[p]}</button>
                ))}
              </div>
              <div class="conteggi" role="group" data-testid="counts">
                {EVENT_TYPES.map((t) => (
                  <button key={t} class="conteggio" aria-pressed={types.includes(t) ? 'true' : 'false'} onClick={() => toggle(t)} data-type={t}>
                    <EvMark type={t} /><span class="l">{S.evTypes[t]}</span><span class="n">{counts[t]}</span>
                  </button>
                ))}
              </div>
              <p class="piccolo" role="status">{events.length ? S.evTotal(events.length) : S.evNone}</p>
              <label class="riga-check">
                <input type="checkbox" checked={points} onChange={(e) => setPoints(e.currentTarget.checked)} data-testid="show-points" />
                {S.showPoints}
              </label>
              <p class="piccolo">{S.safetyHelpQuasi}</p>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
