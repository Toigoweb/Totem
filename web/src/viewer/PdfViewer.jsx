// Visualizzatore pdf.js: pagine in colonna adattate alla larghezza, scorrimento verticale,
// zoom con pulsanti, Ctrl+rotella e pinch, trascinamento con il mouse, swipe orizzontale
// per cambiare pagina. Si disegnano solo le pagine vicine a quelle visibili.
import { useEffect, useImperativeHandle, useLayoutEffect, useRef, useState } from 'preact/hooks';
import { loadPdfjs, PDF_OPTIONS } from './pdfjs.js';

const MIN_ZOOM = 0.5;
const MAX_ZOOM = 5;
const GAP = 16;
const MAX_CANVAS_PIXELS = 16e6;

const clampZoom = (z) => Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, z));

export function PdfViewer({ data, onLoad, onPage, onError, loadingText, apiRef }) {
  const box = useRef(null);
  const inner = useRef(null);
  const docRef = useRef(null);
  const pagesRef = useRef([]); // PDFPageProxy
  const canvases = useRef([]);
  const rendered = useRef(new Map()); // indice → scala disegnata
  const tasks = useRef(new Map());
  const anchor = useRef(null); // posizione da mantenere dopo lo zoom
  const [sizes, setSizes] = useState(null); // [{w,h}] a scala 1
  const [width, setWidth] = useState(0);
  const [zoom, setZoom] = useState(1);
  const [page, setPage] = useState(1);
  const zoomRef = useRef(1);
  zoomRef.current = zoom;

  // ---- caricamento ----
  useEffect(() => {
    let alive = true;
    let task;
    (async () => {
      try {
        const pdfjs = await loadPdfjs();
        if (!alive) return;
        task = pdfjs.getDocument({ data: new Uint8Array(data.slice(0)), ...PDF_OPTIONS });
        const doc = await task.promise;
        if (!alive) return;
        docRef.current = doc;
        const pages = [];
        const sz = [];
        for (let i = 1; i <= doc.numPages; i++) {
          const p = await doc.getPage(i);
          const vp = p.getViewport({ scale: 1 });
          pages.push(p);
          sz.push({ w: vp.width, h: vp.height });
        }
        if (!alive) return;
        pagesRef.current = pages;
        setSizes(sz);
        onLoad && onLoad(doc.numPages);
      } catch (e) {
        if (alive) onError && onError(e);
      }
    })();
    return () => {
      alive = false;
      for (const t of tasks.current.values()) t.cancel();
      tasks.current.clear();
      if (task) task.destroy();
    };
  }, [data]);

  // ---- larghezza disponibile ----
  useEffect(() => {
    const el = box.current;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth));
    ro.observe(el);
    setWidth(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const maxW = sizes ? Math.max(...sizes.map((s) => s.w)) : 1;
  const fit = width > 0 ? (width - 2 * GAP) / maxW : 1;
  const scale = fit * zoom;

  // ---- disegno delle pagine visibili ----
  const draw = (i) => {
    const p = pagesRef.current[i];
    const c = canvases.current[i];
    if (!p || !c || rendered.current.get(i) === scale) return;
    const old = tasks.current.get(i);
    if (old) old.cancel();
    let dpr = window.devicePixelRatio || 1;
    const vp0 = p.getViewport({ scale: scale });
    if (vp0.width * vp0.height * dpr * dpr > MAX_CANVAS_PIXELS) dpr = Math.sqrt(MAX_CANVAS_PIXELS / (vp0.width * vp0.height));
    const vp = p.getViewport({ scale: scale * dpr });
    const off = document.createElement('canvas');
    off.width = Math.floor(vp.width);
    off.height = Math.floor(vp.height);
    const task = p.render({ canvas: off, viewport: vp });
    tasks.current.set(i, task);
    task.promise.then(
      () => {
        // si sostituisce il contenuto solo a disegno finito: niente pagine bianche durante lo zoom
        c.width = off.width;
        c.height = off.height;
        c.getContext('2d').drawImage(off, 0, 0);
        off.width = off.height = 0;
        rendered.current.set(i, scale);
        tasks.current.delete(i);
      },
      () => tasks.current.delete(i), // annullato: nessun errore
    );
  };

  const visibleRange = () => {
    const el = box.current;
    if (!el || !sizes) return [0, -1];
    const top = el.scrollTop - el.clientHeight;
    const bottom = el.scrollTop + 2 * el.clientHeight;
    let y = GAP;
    let first = -1;
    let last = -1;
    for (let i = 0; i < sizes.length; i++) {
      const h = sizes[i].h * scale;
      if (y + h >= top && y <= bottom) {
        if (first < 0) first = i;
        last = i;
      }
      y += h + GAP;
    }
    return [first, last];
  };

  const update = () => {
    const el = box.current;
    if (!el || !sizes) return;
    const [a, b] = visibleRange();
    for (let i = 0; i < sizes.length; i++) {
      if (i >= a && i <= b) draw(i);
      else if (rendered.current.has(i) && (i < a - 3 || i > b + 3)) {
        const c = canvases.current[i];
        if (c) c.width = c.height = 0; // libera memoria
        rendered.current.delete(i);
      }
    }
    // pagina corrente: quella che occupa il primo terzo dello schermo
    const mark = el.scrollTop + el.clientHeight / 3;
    let y = GAP;
    let cur = 1;
    for (let i = 0; i < sizes.length; i++) {
      if (y <= mark) cur = i + 1;
      y += sizes[i].h * scale + GAP;
    }
    setPage(cur);
  };

  useEffect(() => {
    onPage && onPage(page);
  }, [page]);

  // dopo un cambio di scala si riposiziona sullo stesso punto e si ridisegna
  useLayoutEffect(() => {
    const el = box.current;
    if (!el || !sizes) return;
    const a = anchor.current;
    if (a) {
      el.scrollTop = a.top * scale - a.cy;
      el.scrollLeft = a.left * scale - a.cx;
      anchor.current = null;
    }
    update();
  }, [scale, sizes]);

  const setZoomAround = (z, cx, cy) => {
    const el = box.current;
    const nz = clampZoom(z);
    if (!el || nz === zoomRef.current) return;
    if (cx === undefined) {
      cx = el.clientWidth / 2;
      cy = el.clientHeight / 3;
    }
    anchor.current = { top: (el.scrollTop + cy) / scale, left: (el.scrollLeft + cx) / scale, cx, cy };
    setZoom(nz);
  };

  const goTo = (n) => {
    const el = box.current;
    if (!el || !sizes) return;
    const k = Math.min(sizes.length, Math.max(1, n));
    let y = GAP;
    for (let i = 0; i < k - 1; i++) y += sizes[i].h * scale + GAP;
    el.scrollTop = y - GAP / 2;
    setPage(k);
  };

  useImperativeHandle(apiRef, () => ({
    goTo,
    next: () => goTo(page + 1),
    prev: () => goTo(page - 1),
    zoomIn: () => setZoomAround(zoomRef.current * 1.25),
    zoomOut: () => setZoomAround(zoomRef.current / 1.25),
    fit: () => setZoomAround(1),
    get page() {
      return page;
    },
  }));

  // ---- input: rotella, mouse, tocco ----
  useEffect(() => {
    const el = box.current;
    const onWheel = (e) => {
      if (!e.ctrlKey) return; // rotella semplice: scorrimento nativo
      e.preventDefault();
      const r = el.getBoundingClientRect();
      setZoomAround(zoomRef.current * Math.exp(-e.deltaY * 0.0025), e.clientX - r.left, e.clientY - r.top);
    };
    el.addEventListener('wheel', onWheel, { passive: false });

    // trascinamento con il mouse
    let drag = null;
    const pd = (e) => {
      if (e.pointerType !== 'mouse' || e.button !== 0) return;
      drag = { x: e.clientX, y: e.clientY, l: el.scrollLeft, t: el.scrollTop };
      el.setPointerCapture(e.pointerId);
      el.classList.add('trascina');
    };
    const pm = (e) => {
      if (!drag) return;
      el.scrollLeft = drag.l - (e.clientX - drag.x);
      el.scrollTop = drag.t - (e.clientY - drag.y);
    };
    const pu = () => {
      drag = null;
      el.classList.remove('trascina');
    };
    el.addEventListener('pointerdown', pd);
    el.addEventListener('pointermove', pm);
    el.addEventListener('pointerup', pu);
    el.addEventListener('pointercancel', pu);

    // tocco: pinch (due dita) e swipe orizzontale (un dito)
    let pinch = null;
    let swipe = null;
    const dist = (t) => Math.hypot(t[0].clientX - t[1].clientX, t[0].clientY - t[1].clientY);
    const ts = (e) => {
      if (e.touches.length === 2) {
        const r = el.getBoundingClientRect();
        const cx = (e.touches[0].clientX + e.touches[1].clientX) / 2 - r.left;
        const cy = (e.touches[0].clientY + e.touches[1].clientY) / 2 - r.top;
        pinch = { d0: dist(e.touches), z0: zoomRef.current, cx, cy, ratio: 1 };
        inner.current.style.transformOrigin = `${el.scrollLeft + cx}px ${el.scrollTop + cy}px`;
        swipe = null;
      } else if (e.touches.length === 1) {
        swipe = { x: e.touches[0].clientX, y: e.touches[0].clientY, t: Date.now() };
      }
    };
    const tm = (e) => {
      if (pinch && e.touches.length === 2) {
        e.preventDefault();
        const z = clampZoom(pinch.z0 * (dist(e.touches) / pinch.d0));
        pinch.ratio = z / pinch.z0;
        inner.current.style.transform = `scale(${pinch.ratio})`; // anteprima fluida
      }
    };
    const te = (e) => {
      if (pinch && e.touches.length < 2) {
        inner.current.style.transform = '';
        setZoomAround(pinch.z0 * pinch.ratio, pinch.cx, pinch.cy);
        pinch = null;
        return;
      }
      if (swipe && e.changedTouches.length === 1 && e.touches.length === 0) {
        const t = e.changedTouches[0];
        const dx = t.clientX - swipe.x;
        const dy = t.clientY - swipe.y;
        const noHScroll = el.scrollWidth <= el.clientWidth + 2;
        if (noHScroll && Math.abs(dx) > 80 && Math.abs(dy) < 60 && Date.now() - swipe.t < 700) {
          box.current.dispatchEvent(new CustomEvent('swipepage', { detail: dx < 0 ? 1 : -1 }));
        }
        swipe = null;
      }
    };
    el.addEventListener('touchstart', ts, { passive: true });
    el.addEventListener('touchmove', tm, { passive: false });
    el.addEventListener('touchend', te);
    el.addEventListener('touchcancel', te);
    return () => {
      el.removeEventListener('wheel', onWheel);
      el.removeEventListener('pointerdown', pd);
      el.removeEventListener('pointermove', pm);
      el.removeEventListener('pointerup', pu);
      el.removeEventListener('pointercancel', pu);
      el.removeEventListener('touchstart', ts);
      el.removeEventListener('touchmove', tm);
      el.removeEventListener('touchend', te);
      el.removeEventListener('touchcancel', te);
    };
  }, [scale]);

  useEffect(() => {
    const el = box.current;
    const h = (e) => goTo(page + e.detail);
    el.addEventListener('swipepage', h);
    return () => el.removeEventListener('swipepage', h);
  });

  return (
    <div class="vista" ref={box} onScroll={update} data-testid="viewer" data-page={page} data-zoom={zoom.toFixed(2)}>
      <div class="fogli" ref={inner}>
        {sizes && sizes.map((s, i) => (
          <div class="foglio" key={i} data-page={i + 1} style={{ width: `${Math.floor(s.w * scale)}px`, height: `${Math.floor(s.h * scale)}px` }}>
            <canvas ref={(c) => (canvases.current[i] = c)} />
          </div>
        ))}
      </div>
      {!sizes && <div class="msg">{loadingText}</div>}
    </div>
  );
}
