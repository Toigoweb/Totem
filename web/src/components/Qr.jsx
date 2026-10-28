// Codice QR disegnato in SVG nel pacchetto (nessun servizio esterno).
import qrcode from 'qrcode-generator';

export function qrPath(text) {
  const q = qrcode(0, 'M');
  q.addData(text);
  q.make();
  const n = q.getModuleCount();
  let d = '';
  for (let y = 0; y < n; y++) {
    for (let x = 0; x < n; x++) if (q.isDark(y, x)) d += `M${x + 4} ${y + 4}h1v1h-1z`;
  }
  return { n: n + 8, d };
}

export function Qr({ text, label }) {
  const { n, d } = qrPath(text);
  return (
    <svg class="qr" viewBox={`0 0 ${n} ${n}`} role="img" aria-label={label} shape-rendering="crispEdges" data-testid="qr">
      <rect width={n} height={n} fill="#fff" />
      <path d={d} fill="#000" />
    </svg>
  );
}
