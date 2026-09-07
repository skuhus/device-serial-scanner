# Replay captures

Byte-for-byte streams as they arrive on the wire from a scanner in USB-CDC
mode, replayed into a pseudo-terminal by the tests in this package.

`symbol-05e0-1701-crlf.bin` holds real payloads read off a Symbol 05e0:1701 on
2026-09-06, each followed by the CRLF that device transmits. The payload bytes
are exactly what the scanner sent; the terminators are reconstructed from the
confirmed suffix rather than copied out of a raw dump, and the reconstruction
cross-checks against the logged read sizes (14+2=16, 9+2=11, 35+2=37). See
`docs/scanners/symbol-05e0-1701.md`.

The other four files are synthesised, not captured from hardware. They cover the
shapes the framer has to survive, but they are not evidence about any specific
scanner model. Replace each one with a real capture as the model reaches the
fleet, and add a file per model: a replacement scanner that suffixes CRLF where
the previous one suffixed CR is the failure this directory exists to catch.

To capture from a real device:

    cat /dev/serial/by-id/usb-YOUR_SCANNER-if00 > model-name.bin

Scan the sample codes, then interrupt. Keep the raw bytes; do not edit the file
in a text editor, which will rewrite the line endings and destroy the thing
being tested.

| File | Contents |
|---|---|
| `code128-cr.bin` | one numeric payload, CR terminated |
| `gs1-128-cr.bin` | AIM identifier `]C1` and two 0x1D group separators, CR terminated |
| `burst-crlf.bin` | three scans back to back, CRLF terminated |
| `non-utf8-cr.bin` | payload containing bytes that are not valid UTF-8 |
| `symbol-05e0-1701-crlf.bin` | real Symbol 05e0:1701 payloads, CRLF terminated, 1D and 2D |

No GS1-128 payload has been captured from hardware yet: none of the barcodes to
hand were GS1, so `gs1-128-cr.bin` is still synthesised. That is the one
remaining gap, and it matters most, because 0x1D is the byte a text-based
implementation destroys.
