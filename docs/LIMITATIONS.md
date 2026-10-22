# Known gaps and decisions needed

Be explicit about what is verified and what is not.

## Verified against the real artifacts
* DOF effect engine semantics were ported from the DirectOutput source (`Configurator`,
  `TableConfigSetting`, all timed/value/condition effects, toy layers, alpha blending, PinOne
  controller loop). The real 800 KB `directoutputconfig11.ini` parses (1661 tables).
* `DOFLinxTrigger.dll` was executed in a harness: pipe name, record framing and message set are
  exactly as implemented (round trip tests use bytes captured from the DLL).
* FX/Linx command files: all 140+ shipped `.FX` files parse; commands from All_Pre, the table file
  and All_Post accumulate in that order; `[CLEAR COMMANDS]` and `ALIAS` are honoured.
* End to end smoke test: real `DOFLinxTrigger.dll` → pipe → FX files → DOF ini → PinOne packets
  (dry run log). **Not tested against a physical PinOne or a running game on this machine.**

## Not possible / needs your decision

1. **Pinball FX3 / Classic memory triggers are encrypted.** In `*.FX3` files the memory trigger
   lines are opaque base64 blobs (AES-like) that only DOFLinx can read; the key lives inside the
   obfuscated `DOFLinx.exe`. SuperDOF detects and reports this but does not break it. Options:
   obtain plaintext triggers from the DOFLinx author, or have the key extracted (that is
   circumventing the author's protection, so it was not done without your say-so).
2. **Game id → table file for Pinball FX.** The ids the game sends (`G,<number>`) are the official Pinball FX
   table numbers (published by Zen Studios); they are built in (`internal/linx/pfxtables.go`, matched to the
   DOFLinx 10.03 `.FX` names). New tables released later need an entry in `tables.ini` (`<id>=<TableFileName>`)
   or an assignment on the game id page (http://127.0.0.1:8765/), which override the built-in list. Tables
   without a DOFLinx file (System Shock, Camp Bloodbrook at the time of writing) only get All_Pre/All_Post.
   Pinball M / IMP ids are not covered. Never guessed: unknown ids are never auto-assigned.
3. **FX2 table-based memory triggers** use a pointer chain whose semantics are inferred from the
   shipped files (documented in PROTOCOLS.md). It matches the structure of every file but could
   not be confirmed without running Pinball FX2.
4. **Future Pinball link** (shared-memory handshake with `DOFLinx.vbs`) is not implemented.
5. **DOF event duration for `FF_DOF Exxx,-1`.** The docs only say "predefined duration". SuperDOF
   turns the element on, and switches it off again after the longest self terminating duration of
   the ini effects attached to it (stays on if none); `FF_DOF Exxx,0` turns it off.
6. **DOF2DMD, Pixelcade, SSF sound, B2S animations, MAME** are out of scope by design (hardware
   feedback only).
7. **Single PinOne** (63 outputs); DOFLinx addresses (`DOOO`) only reach it through `pinOneDevice`
   (default 1), other device numbers are ignored with a warning. DOF toys/ini columns can still
   address strips (LedWiz 30) and FTDI chips (40+) normally.
8. **LED strips / FTDI are verified against a protocol simulator, not real hardware.** The Teensy/Wemos
   handshake and data commands, the matrix effects (color, shift, flicker, plasma, shapes, bitmaps,
   animations) and the FT245R bit-bang were ported from the DOF source and tested with fake
   devices. GIF/PNG/JPEG bitmaps are supported (no BMP). FTDI needs the FTDI D2XX driver
   (ftd2xx.dll) and is Windows only; the strip serial link works on Windows and Linux.
9. **Mixed DOFLinx + strips:** DOFLinx `FF_PC` (Pixelcade LED strip) commands are not DOF strips and
   are ignored.
10. macOS: builds, but USB/serial access is not implemented.
11. **Tester:** a simulated game (`superdof test`) runs the command files and DOF effects but does not poll
    game memory, so FX2/FX3 memory triggers and the game id mapping are not exercised; use the console
    (`E,NAME,1` triggers) to feed events. Strip patterns are verified against the protocol simulator, not a physical strip.
