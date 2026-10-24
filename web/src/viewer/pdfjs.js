// pdf.js self-hosted: libreria, worker, font standard, cmaps e decoder wasm vengono
// dall'app stessa (copiati in /pdfjs/ dalla build), nessuna CDN.
import workerUrl from 'pdfjs-dist/build/pdf.worker.min.mjs?url';

let loading;

export function loadPdfjs() {
  if (!loading) {
    loading = import('pdfjs-dist').then((pdfjs) => {
      pdfjs.GlobalWorkerOptions.workerSrc = workerUrl;
      return pdfjs;
    });
  }
  return loading;
}

export const PDF_OPTIONS = {
  isEvalSupported: false,
  cMapUrl: '/pdfjs/cmaps/',
  cMapPacked: true,
  standardFontDataUrl: '/pdfjs/standard_fonts/',
  wasmUrl: '/pdfjs/wasm/',
  iccUrl: '/pdfjs/iccs/',
  enableXfa: false,
};
