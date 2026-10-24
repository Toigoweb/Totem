// Icone SVG inline. Le prime 10 sono il set fisso delle sezioni.
const P = {
  documento: <><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><path d="M14 3v5h5M9 13h6M9 17h6" /></>,
  flacone: <><path d="M9 3h6M10 3v5L5 18a2 2 0 0 0 1.8 3h10.4A2 2 0 0 0 19 18l-5-10V3" /><path d="M7.5 14h9" /></>,
  scudo: <><path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z" /><path d="M9 12l2 2 4-4" /></>,
  fiamma: <path d="M12 3c1 4 5 6 5 11a5 5 0 0 1-10 0c0-2 1-3.5 2-4.5 0 2 1 3 2 3 0-3-1-5 1-9.5z" />,
  croce: <><rect x="3" y="3" width="18" height="18" rx="3" /><path d="M12 7v10M7 12h10" /></>,
  casco: <><path d="M3 17h18M4 17a8 8 0 0 1 16 0" /><path d="M10 9V6h4v3M12 6v5" /><path d="M3 17v2h18v-2" /></>,
  uscita: <><path d="M14 3H5v18h9" /><path d="M10 12h11M17 8l4 4-4 4" /></>,
  mappa: <><path d="M9 4L3 6v14l6-2 6 2 6-2V4l-6 2z" /><path d="M9 4v14M15 6v14" /></>,
  ingranaggio: <><circle cx="12" cy="12" r="3" /><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1L7 17M17 7l2.1-2.1" /></>,
  cartella: <path d="M3 6a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v10a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />,

  cerca: <><circle cx="11" cy="11" r="7" /><path d="M20 20l-4-4" /></>,
  spunta: <><circle cx="12" cy="12" r="9" /><path d="M8 12l3 3 5-6" /></>,
  check: <path d="M5 12l5 5 9-10" />,
  sync: <><path d="M20 11a8 8 0 0 0-14.5-4M4 13a8 8 0 0 0 14.5 4" /><path d="M5 3v4h4M19 21v-4h-4" /></>,
  nuvola: <path d="M7 18h10a4 4 0 0 0 1-7.9A6 6 0 0 0 7.3 8.2 5 5 0 0 0 7 18z" />,
  carica: <><path d="M12 16V4M7 9l5-5 5 5" /><path d="M4 16v3a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-3" /></>,
  rete: <><rect x="3" y="4" width="18" height="6" rx="1" /><rect x="3" y="14" width="18" height="6" rx="1" /><path d="M7 7h.01M7 17h.01" /></>,
  nuvolaOff: <><path d="M7 18h10a4 4 0 0 0 1-7.9A6 6 0 0 0 7.3 8.2 5 5 0 0 0 7 18z" /><path d="M3 3l18 18" /></>,
  avviso: <><path d="M12 3l10 18H2z" /><path d="M12 10v5M12 18v.5" /></>,
  indietro: <path d="M15 5l-7 7 7 7" />,
  avanti: <path d="M9 5l7 7-7 7" />,
  pdf: <><path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z" /><path d="M14 3v5h5" /><path d="M8 16v-3h1.5a1 1 0 0 1 0 2H8M12 13v3h1a1.5 1.5 0 0 0 0-3zM16 16v-3h2M16 14.5h1.5" /></>,
  offline: <><path d="M12 4v11M8 11l4 4 4-4" /><path d="M5 20h14" /></>,
  orologio: <><circle cx="12" cy="12" r="9" /><path d="M12 7v5l3 2" /></>,
  piu: <path d="M12 5v14M5 12h14" />,
  meno: <path d="M5 12h14" />,
  su: <path d="M6 15l6-6 6 6" />,
  giu: <path d="M6 9l6 6 6-6" />,
  matita: <path d="M4 20h4L19 9l-4-4L4 16z" />,
  cestino: <path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13" />,
  lucchetto: <><rect x="5" y="11" width="14" height="10" rx="2" /><path d="M8 11V8a4 4 0 0 1 8 0v3" /></>,
  x: <path d="M6 6l12 12M18 6L6 18" />,
  errore: <><circle cx="12" cy="12" r="9" /><path d="M9 9l6 6M15 9l-6 6" /></>,
  adatta: <path d="M4 9V4h5M20 9V4h-5M4 15v5h5M20 15v5h-5" />,
  testo: <><path d="M4 18l5-12 5 12M6 14h6" /><path d="M17 9v6M14 12h6" /></>,
  info: <><circle cx="12" cy="12" r="9" /><path d="M12 11v6M12 7.5v.5" /></>,
};

export const SECTION_ICONS = ['documento', 'flacone', 'scudo', 'fiamma', 'croce', 'casco', 'uscita', 'mappa', 'ingranaggio', 'cartella'];

export function Icon({ name, class: cls = '', ...rest }) {
  return (
    <svg class={`i ${cls}`} viewBox="0 0 24 24" aria-hidden="true" focusable="false" {...rest}>
      {P[name] || P.documento}
    </svg>
  );
}
