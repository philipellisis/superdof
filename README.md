# SuperDOF

One program that replaces **DOF + DOFLinx** for a **PinOne** cabinet, including **addressable LED
strips** (Wemos D1 mini / Teensy) and **FTDI FT245R** relay boards:

* reads one **`superdof.json`** (or your existing `DOFLinx.INI` + DOF `cabinet.xml`),
* keeps using your **`.FX` / `.FX2` / `.FX3` / `.Linx`** files and the DOF **`directoutputconfigNN.ini`**
  (LedControl) files, `DirectOutputShapes.xml/.png` and `tablemappings.xml`,
* listens to Pinball FX, Pinball FX2/FX3/Classic, Pinball M, Infected Mushroom Pinball and **VPX**,
* drives the PinOne (USB HID or legacy serial), the LED strips and the FTDI chips.

Go, standard library only, one static executable. Windows is fully supported; Linux builds and
supports PinOne HID/serial, strips and the VPX bridge protocol; macOS compiles but has no USB.

```
Pinball FX / M / IMP ──DOFLinxTrigger.dll──► \\.\pipe\DOFLinxTrigger ─┐
Pinball FX2 / FX3  ───process memory────────────────────────────────┤
VPX ─► B2S server ─► B2SServerDirectOutputPlugin.dll ─► \\.\pipe\SuperDOF ┤
DOFLinxMsg / front end ───────────────────► \\.\pipe\DOFLINX ───────┤
                                                                     ▼
                         DOFLinx layer (FX/Linx commands, LINK_xx devices, RGB, flashers, buttons)
                                                                     ▼
              DOF engine (ledcontrol ini → effect chains → toys/layers/matrix → outputs)
                       ▼                      ▼                       ▼
                    PinOne            Wemos / Teensy strips       FTDI FT245R
```

## Running in the background (Windows)

`superdof.exe` is built for the Windows GUI subsystem: started from `shell:startup`, Explorer or a shortcut it opens **no
window**. With no console it writes its log to `%ProgramData%SuperDOFsuperdof.log` (the previous run is kept as
`superdof.log.1`; `-log FILE` chooses another file). Started from a command prompt it attaches to that console and
prints as usual. A prompt does not wait for a program like this, so for `setup`, `devices`, `-check` and other short
commands use **`superdof-console.exe`** (the same program built as a console application), or `start /wait superdof ...`.

## Quick start (Windows)

1. Build (Go 1.22+): `powershell ./build.ps1` or `go build -o superdof.exe ./cmd/superdof`
2. Create the configuration from what you already have:
   `superdof convert -ini C:\DOFLinx\config\DOFLinx.INI -o superdof.json`
   (also reads `cabinet.xml` and `GlobalConfig.xml`; see `superdof.sample.json` for the format)
3. Check it: `superdof -config superdof.json -check` (add `-dry-run` to run without touching hardware)
4. Run it: `superdof -config superdof.json` and leave it running; it detects the games itself.

`superdof devices` lists the PinOne, serial ports and FTDI chips it can see.

## Pinball FX / Pinball M / Infected Mushroom: the trigger DLL

These games only offer the *DOFLinx* option in **Settings > Cabinet** when `DOFLinxTrigger.dll` sits next to
their executable (the DOFLinx documentation's manual step). SuperDOF does this for you:

* at start-up (unless `AUTO_INSTALL_TRIGGER=0`) it finds the Steam libraries, locates every installed
  Pinball FX / M / IMP and copies the DLL next to the `*-Win64-Shipping.exe` (updating it when it differs),
* or run it yourself: `superdof install-trigger [-dll F] [-game FOLDER]` (the Tester has the same under `/api/trigger`).

The DLL is the unmodified `DOFLinxTrigger.dll` from the DOFLinx 10.03 download (by DDH69), **built into superdof.exe**,
so nothing needs to be downloaded or placed by hand. `TRIGGER_DLL` / `-dll` use your own copy instead (for a newer
DOFLinx release). Extra game folders can be listed in `GAME_DIR`. Writing into `Program Files` can need administrator rights once.
Then start the game, turn cabinet mode on, open the DOFLinx tab and enable it, with superdof running.
The pipes accept any local signed-in user, so superdof and the game need not both be (or both not be)
elevated; network clients are refused.

## One command setup

`superdof setup` installs what the games need and finds the folders itself (`-fp` / `-b2s` override):

| game | what is installed |
|---|---|
| Pinball FX / M / Infected Mushroom | the embedded `DOFLinxTrigger.dll` next to the game |
| Future Pinball | the embedded `DOFLinx.vbs` into `<Future Pinball>\Scripts` (an older different file is kept as `.bak`) |
| Visual Pinball | the embedded B2S bridge plugin in `%ProgramData%SuperDOFB2SPlugin`, plus a `SuperDOF.lnk` shortcut to it in the `Plugins` and `Plugins64` folders of every B2S server found; the `DirectOutput.lnk` shortcuts there are renamed to `DirectOutput.lnk.superdof-off` so DOF and superdof do not both drive the PinOne (`superdof restore-vpx` undoes it) |

The running service does the same at start-up; switch off with `AUTO_INSTALL_TRIGGER=0`, `AUTO_INSTALL_FP=0`, `AUTO_INSTALL_VPX=0`.
Writing into Program Files can need an administrator shell once. With the bridge installed, VPX feedback only works
while superdof is running.

**PinUP Popper**: the Popper menu sends DOF events through the COM object `DirectOutput.ComObject` (its script creates
`PinUPDOF.DOFLink`, which creates `DirectOutput.ComObject`). `setup` (or `superdof install-popper`) registers a
replacement with the same CLSID and methods that forwards to superdof. It is registered for the current user only (no
admin) and `superdof restore-popper` removes it so a DirectOutput install is used again. In Popper set `useDOF = true`
(`PUPMenuScriptSysOptions.txt`) so the menu sends events; the ROM name used is `pinupmenu`.

**Future Pinball** tables that call `LoadExternalScript("DOFLinx.vbs")` and `FF_Init` talk to superdof through the
game memory (the `DDH_Slots` array of the script): superdof finds it, tells the table which devices your cabinet has
(the others keep playing their normal sound), then runs the `FF_Dev`, `FF_Flasher`, `FF_Button`, `FF_Colour`,
`FF_ROM` and `FF_DOF` calls of the table. `FP_ATTEMPT_LINK=0` turns the link off. Tables without DOFLinx support are
not affected.

## Game ids (Pinball FX)

Pinball FX tells DOFLinx which table is selected with its official table number. SuperDOF has the list built in
(143 tables matched to the DOFLinx `.FX` files), so tables are recognised automatically. The page
http://127.0.0.1:8765/ (served by the running superdof, `-ui off` to disable) shows each id as you browse the menu,
with its official name, and lets you override or add one (saved in `tables.learned.ini`; `tables.ini` also works).
Set `PATH_FX=` to the DOFLinx `FX` folder; if it is missing, superdof searches the usual places.

## No DOFLinx.INI needed

The DOF Config Tool writes the name of every output as a comment under `[Config DOF]` in
`directoutputconfig11.ini` (and the FTDI file), e.g. `# "Rom","Flipper Left","Slingshot Left",...`. SuperDOF reads
that line and builds the DOFLinx devices from it: Flipper Left is `LF` on the port where the name sits, bumpers are
`ML/MC/MR` (middle) and `BL/BC/BR` (back), flashers `FLOL/FLIL/FLCN/FLIR/FLOR`, `Strobe` is `SR`,
Start/Extra Ball/Coin/Launch/Fire are the `BUT_` buttons, magnasaves `BUT_ML/BUT_MR`, Shaker/Gear/Knocker
`SH/GR/KN`, and `RGB Flippers` is `RGB_OUTPUT`. RGB toys use the first of their three ports. The layout is whatever
your file says, so nothing is assumed about the cabinet. The PinOne file (11) is DOFLinx device `PINONE_DEVICE`
(default 1); FTDI files are devices `FTDI_DEVICE` (default 2) upwards, in file order.

* A `LINK_xx` / `RGB_OUTPUT` in `DOFLinx.INI` or `superdof.json` still wins; superdof warns when it differs from the ini.
* `AUTO_LINKS=0` turns this off. Names it does not know (e.g. a Fan) are listed in the log, never guessed.
* The Tester shows each link with the toy name it came from.
* Addressable LED strips need no `LINK_` lines: FX tables drive them with `FF_DOF E...` effects of the `doflinx`
  ROM in `directoutputconfig30.ini`, through the strip toys of `cabinet.xml` (which holds the port and LED counts).

## Troubleshooting outputs

* A new link always starts from silence: a new ROM from VPX / Popper, a new Pinball FX / FX2 / FX3 table or a Future Pinball session switches every output off first, and so does a front end that closes or is killed without saying goodbye.
* `superdof -debug` logs every event from the games and front ends (bridge messages, DOFLinx actions and commands).
* `superdof -trace-outputs 11,12,13` (or `all`) logs every change of those outputs with what holds them: the value from the DOF effects and the DOFLinx sources. Same as `DEBUG=1` / `TRACE_OUTPUTS=` in the config. Together they show which event left an output on. With `-trace-outputs` the PinOne packets that carry those outputs are logged as well, so the log shows what was actually sent to the board.
* PinOne packets are sent at least 8 ms apart unless `pinOneMinCommandIntervalMs` / `PinOneDefaultMinCommandIntervalMs` says otherwise: the V2 firmware needs about 7 ms to read one 63 byte HID report, and faster bursts overflowed its receive queue.
* The complete output state is resent to the PinOne every 2 s even when nothing changed (`PINONE_REFRESH_MS`, `-1` = never), so an output that the board got wrong or lost cannot stay on for long.

## Tester

`superdof test` opens a local web page (loopback only) that runs the real engine so you can try a
configuration before a game does:

```
superdof test                                   # then fill in the Setup tab (detects common folders)
superdof test -dof-config C:\DirectOutput\Config -ini C:\DirectOutput\Config\DOFLinx.INI
superdof test -config superdof.json -dry-run    # simulation: nothing touches hardware
```

No `GlobalConfig_B2SServer.xml` or B2S server is involved. Inputs: a `superdof.json`, or a
`DOFLinx.INI` (+ `cabinet.xml`), or just the folder with the `directoutputconfig*.ini` files with
*auto configure* ticked (a PinOne, FTDI chips found, and LedWiz equivalents are built for you).

| tab | what it does |
|---|---|
| Setup | pick files, simulation or live, summary of ini files (with warning counts), controllers |
| Outputs | live value bar for every output of every controller; click to force one on, *Walk* pulses a range in turn |
| Toys & LED strips | per toy tests through the real path (curve, colour order, brightness, mapping): RGB colours, strip patterns (thirds to verify colour order, rainbow, gradient, single LED, chase); live pixel preview of each strip controller |
| ROM / elements | search the ~1600 ROM configs, load one, see its elements and the effects attached, fire them (pulse like `FF_DOF Exxx,-1`, on, off, N ms, value) and watch which outputs react |
| DOFLinx | simulate a game (`All_Pre` + table file + `All_Post`, startup actions, `FF_ROM`), fire its events, console for `FF_...` actions, game triggers (`E,`/`S,`/`G,`) and real time commands; table of every `LINK_xx` address showing whether the PinOne can reach it |
| Log | everything the engine and DOFLinx layer report |

The red **ALL OFF** button releases test outputs, toy tests, DOFLinx outputs, the simulated game and the ROM.
In live mode the page asks before loading and before *Walk*; do not hold solenoid outputs on.

## Is it a single application?

Yes: `superdof.exe` is the whole runtime (DOF engine, DOFLinx logic, hardware drivers, pipe servers).
Only two things live outside it, because they have to be loaded *by other programs*:

| file | why it must stay separate |
|---|---|
| `DOFLinxTrigger.dll` | loaded by Pinball FX / Pinball M / IMP itself as their cabinet plugin; the unmodified DOFLinx file is **embedded** in `superdof.exe` and copied next to the game automatically |
| `B2SServerDirectOutputPlugin.dll` | loaded by the B2S server for VPX (through a shortcut in its Plugins folders). The bridge is **embedded** in `superdof.exe`; install it with `superdof install-vpx <B2S folder>` |

No `DirectOutput.dll`, `DOFLinx.exe`, `DOFLinxCommon.dll`, BASS, .NET runtime or COM registration is needed.

## Configuration

`superdof.json` (comments allowed, unknown keys are rejected so typos are caught):

* `pinone` – `auto`, `hid` or a serial port,
* `dof` – ini folder, minimum effect durations, `tablemappings.xml`,
* `cabinet` – `controllers` (`pinone`, `wemosd1`, `teensy`, `ftdi`) and `toys`
  (`ledwizequivalent`, `lamp`, `rgb`, `ledstrip`); a PinOne is auto-added when none is listed,
* `doflinx` – processes, file paths, `pinOneDevice`, `forceTable`, `tables` (game id → table),
  `devices` (LF, RF, …), `flashers`, `buttons`, `rgb`, `fixedOutputs`, `colourOrder`, and `ini`
  for any other raw DOFLinx.INI key.

The old formats still work: `-ini DOFLinx.INI [-cabinet cabinet.xml]`.

LedWizEquivalent numbers follow DOF: PinOne unit *n* = `10+n` (`directoutputconfig11.ini`),
FTDI chip *i* = `40+i`, strips = whatever number your `ledwizequivalent` toy has (30 in the usual
setup).

## Command line

```
superdof [flags]                     run (flags: -config -ini -cabinet -dof-config -pinone -dry-run
                                      -table -rom -log -check -test-outputs N -version)
superdof convert -ini F [-cabinet F] -o superdof.json
superdof install-vpx <B2S server folder>
superdof devices
superdof test [-config F | -ini F -cabinet F -dof-config D] [-dry-run] [-listen 127.0.0.1:PORT] [-no-browser]
```

`-test-outputs N` lights PinOne outputs 1..N one after another – careful with solenoid outputs.

## Layout

```
cmd/superdof        the service, convert/install-vpx/devices sub commands
internal/dof        DOF engine: ledcontrol parser, configurator, effects, toys, matrix effects,
                    PinOne / strip / FTDI controllers
internal/pinone     PinOne USB HID / serial transports and packet format
internal/serial     serial port with read timeouts (Windows, Linux)
internal/ftdi       FTDI D2XX driver binding (ftd2xx.dll, Windows)
internal/linx       DOFLinx behaviour: INI/FX parser, command runner, game manager, protocols
internal/config     superdof.json model, INI → JSON conversion
bridge/B2SBridge    C# B2S server plugin shim for VPX (net48), embedded into superdof.exe
docs/               architecture, protocols, limitations
```
