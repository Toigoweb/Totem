// Individua le 16 sezioni standard di una scheda di sicurezza (Reg. CE 1907/2006, all. II).
// Si cercano solo intestazioni a inizio riga ("SEZIONE 4", "SECTION 4"), così i rimandi
// nel testo ("vedere la sezione 8") non contano. L'ordine delle pagine deve crescere.

const HEADING = /^[\s\d.]*(?:SEZIONE|SECTION|SEZ\.)\s*(\d{1,2})(?![\d.,]*\d)/gim;

// Soglia: con almeno 8 sezioni su 16 il documento si considera una SDS riconosciuta.
export const MIN_SECTIONS = 8;

/**
 * @param {string[]} pages testo di ogni pagina, con "\n" a fine riga
 * @returns {{recognized: boolean, sections: Record<number, number>}} sezione → pagina (1-based)
 */
export function findSdsSections(pages) {
  const perPage = pages.map((text) => {
    const found = new Set();
    for (const m of text.matchAll(HEADING)) {
      const n = Number(m[1]);
      if (n >= 1 && n <= 16) found.add(n);
    }
    return found;
  });
  // una pagina con quasi tutte le intestazioni è un sommario: si ignora, se senza di
  // lei il documento resta riconoscibile
  const toc = new Set(perPage.flatMap((f, i) => (f.size >= 12 ? [i] : [])));
  let r = pick(perPage, toc);
  if (toc.size && Object.keys(r).length < MIN_SECTIONS) r = pick(perPage, new Set());
  const recognized = Object.keys(r).length >= MIN_SECTIONS;
  return { recognized, sections: recognized ? r : {} };
}

function pick(perPage, skip) {
  const sections = {};
  let last = 1;
  for (let n = 1; n <= 16; n++) {
    const i = perPage.findIndex((f, idx) => !skip.has(idx) && idx + 1 >= last && f.has(n));
    if (i >= 0) {
      sections[n] = i + 1;
      last = i + 1;
    }
  }
  return sections;
}

/**
 * Estrae il testo delle pagine con pdf.js. Caricato solo quando serve, nel browser.
 * @param {ArrayBuffer} buf
 * @returns {Promise<{pages: string[], numPages: number}>}
 */
export async function extractPagesText(buf) {
  const { loadPdfjs, PDF_OPTIONS } = await import('../viewer/pdfjs.js');
  const pdfjs = await loadPdfjs();
  const task = pdfjs.getDocument({ data: new Uint8Array(buf.slice(0)), ...PDF_OPTIONS });
  const doc = await task.promise;
  const pages = [];
  try {
    for (let i = 1; i <= doc.numPages; i++) {
      const page = await doc.getPage(i);
      const tc = await page.getTextContent();
      pages.push(tc.items.map((it) => (it.str || '') + (it.hasEOL ? '\n' : ' ')).join(''));
      page.cleanup();
    }
    return { pages, numPages: doc.numPages };
  } finally {
    task.destroy();
  }
}

/** Analizza un PDF: non lancia mai, al peggio restituisce "non riconosciuto". */
export async function analyzePdf(buf, extract = extractPagesText) {
  try {
    const { pages, numPages } = await extract(buf);
    return { numPages, ...findSdsSections(pages) };
  } catch (e) {
    console.warn('analisi SDS non riuscita', e);
    return { numPages: 0, recognized: false, sections: {} };
  }
}
