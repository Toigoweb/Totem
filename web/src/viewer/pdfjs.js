// pdf.js self-hosted: libreria e worker vengono dal bundle dell'app, nessuna CDN.
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
