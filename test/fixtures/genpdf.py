#!/usr/bin/env python3
"""Genera PDF di prova senza dipendenze: testo Helvetica (font standard non incorporato),
una riga per voce, una pagina per elenco.

  genpdf.py out.pdf sds "Acetone"      SDS di 6 pagine con le 16 sezioni
  genpdf.py out.pdf plain "Titolo" N   documento generico di N pagine
  genpdf.py out.pdf truncated "X"      PDF senza %%EOF finale
"""
import sys

SDS = ["Identificazione della sostanza o della miscela e della societa", "Identificazione dei pericoli",
       "Composizione/informazioni sugli ingredienti", "Misure di primo soccorso", "Misure di lotta antincendio",
       "Misure in caso di rilascio accidentale", "Manipolazione e immagazzinamento",
       "Controlli dell'esposizione/protezione individuale", "Proprieta fisiche e chimiche", "Stabilita e reattivita",
       "Informazioni tossicologiche", "Informazioni ecologiche", "Considerazioni sullo smaltimento",
       "Informazioni sul trasporto", "Informazioni sulla regolamentazione", "Altre informazioni"]


def esc(t):
    return t.replace("\\", "\\\\").replace("(", "\\(").replace(")", "\\)")


def build(pages):
    objs = []
    objs.append("<< /Type /Catalog /Pages 2 0 R >>")
    kids = " ".join(f"{4 + 2 * i} 0 R" for i in range(len(pages)))
    objs.append(f"<< /Type /Pages /Kids [{kids}] /Count {len(pages)} >>")
    objs.append("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
    for i, lines in enumerate(pages):
        ops = ["BT", "/F1 12 Tf", "14 TL", "56 790 Td"]
        for ln in lines:
            ops.append(f"({esc(ln)}) Tj T*")
        ops.append("ET")
        stream = "\n".join(ops)
        objs.append(f"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents {5 + 2 * i} 0 R >>")
        objs.append(f"<< /Length {len(stream.encode('latin-1'))} >>\nstream\n{stream}\nendstream")
    out = b"%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"
    offs = []
    for n, o in enumerate(objs, 1):
        offs.append(len(out))
        out += f"{n} 0 obj\n{o}\nendobj\n".encode("latin-1")
    x = len(out)
    out += f"xref\n0 {len(objs) + 1}\n0000000000 65535 f \n".encode()
    for o in offs:
        out += f"{o:010d} 00000 n \n".encode()
    out += f"trailer\n<< /Size {len(objs) + 1} /Root 1 0 R >>\nstartxref\n{x}\n%%EOF\n".encode()
    return out


def main():
    path, kind, title = sys.argv[1], sys.argv[2], sys.argv[3]
    if kind == "sds":
        pages, cur = [], [f"SCHEDA DI DATI DI SICUREZZA - {title}", "Conforme al Regolamento (CE) n. 1907/2006", ""]
        for n, name in enumerate(SDS, 1):
            cur += [f"SEZIONE {n}: {name}", f"{n}.1 Testo di esempio della sezione {n}.", "Vedere anche la sezione 8 per i DPI.", ""]
            if n % 3 == 0:
                pages.append(cur)
                cur = []
        if cur:
            pages.append(cur)
    else:
        n = int(sys.argv[4]) if len(sys.argv) > 4 else 2
        pages = [[title, f"Pagina {i + 1}", "Testo di esempio."] for i in range(n)]
    data = build(pages)
    if kind == "truncated":
        data = data[: len(data) // 2]
    open(path, "wb").write(data)


if __name__ == "__main__":
    main()
