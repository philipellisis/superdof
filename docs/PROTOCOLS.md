# Wire protocols

All of this was established from the shipped artifacts (DOF source, and the DOFLinx 10.03 binaries
by running `DOFLinxTrigger.dll` in a throw-away harness and capturing what it writes).

## PinOne board (from DOF `Cab/Out/PinOne`)

Packet (9 bytes): `00, 200+bank, v1 … v7`. Sets the brightness (0-255) of the 7 outputs of one
bank; bank 0 = outputs 1-7 … bank 8 = outputs 57-63. Outputs are brightness only (0 = off).

| firmware | transport | framing |
|---|---|---|
| current | USB HID, VID `0x39F5` PID `0x9208`, usage page `0xFF00` usage `1` | 64 byte output report: report id `6` + packet, zero padded |
| legacy  | USB CDC serial, VID `0x0E8F` PID `0x9207`, 2 000 000 baud 8N1, DTR on | packet written as is |

Minimum interval between packets: 1 ms (`PinOneDefaultMinCommandIntervalMs`). Like DOF the first
change after start sends an all-zero frame, then the current values.

## `DOFLinxTrigger.dll` → `\\.\pipe\DOFLinxTrigger`

The DLL (a .NET 10 NativeAOT library) is loaded by Pinball FX, Pinball M and Infected Mushroom
Pinball. Its exports `OnEvent(char*, int)`, `OnGameChange(int)`, `OnStatusChange(int)`,
`OnNudge(int)`, `OnScoreChange(int, …)` each write one record to the pipe:

```
0x05  <obfuscated text>  0x0D 0x0A
```

`obfuscated[i] = text[i] - (10 + i/7)` (mod 256). Decoded text:

| text | meaning | runner action |
|---|---|---|
| `E,<EVENT>,<value>` | table event, e.g. `E,BUMPER01,1` | `<EVENT>|ON` (value ≠ 0) or `|OFF` |
| `S,Menu` / `S,InLane` / `S,Play` / `S,<n>` | status | `MENU_MODE` / `INLANE_MODE` / `PLAY_MODE` ON, previous mode OFF |
| `N,Left` / `N,Right` / `N,Forward` | nudge | `Nudge_<dir>|ON` (then OFF after 150 ms) |
| `G,<id>` | game/table changed | table lookup through `tables.ini` |
| `C,<player>` | score changed | ignored |

(Captured examples are in `internal/linx/linx_test.go`.)

## DOFLINX command pipe (`DOFLinxMsg.exe`)

Plain text, several commands separated by `#`:
`QUIT`, `OUTPUT_NOW_TIMER=DOOO,ms[,…]`, `OUTPUT_NOW_COLOUR=DOOO,ms,colour[,…]`, `OUTPUTS_OFF`,
`INFO=text`, and any action name from `Menu.ini` (runs its `ON` list).

## SuperDOF bridge pipe (`\\.\pipe\SuperDOF`)

Used by `B2SServerDirectOutputPlugin.dll` (VPX). UTF-8 lines, `|` separated:

```
HELLO                         -> OK SuperDOF <version>
INIT|<tableFile>|<rom>        -> OK | ERR …     (rom empty: looked up in tablemappings.xml)
D|<type>|<number>|<value>                       table element, e.g. D|E|101|1
N|<name>|<value>                                named element ($name)
FINISH                        -> OK
```

## FX2 memory triggers (inferred — see LIMITATIONS)

`[MEMORY TRIGGERS - CORE]` `Name=<hexaddr>,<hexmask>`: value = dword at address; ON when
`value & mask != 0`.
`[MEMORY TRIGGERS - TABLE BASED]` `Name=<base>,<mask>,<o1>,…,<oN>`: `p = [base]`, then
`p = [p + o1] … [p + o(N-1)]`, value = `[p + oN]`. Polled every 5 ms.
