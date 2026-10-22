# Architecture

## What was ported from where

| SuperDOF | origin |
|---|---|
| `internal/dof/ledcontrol.go` | `LedControl/Loader/*` (ini sections, variables, `TableConfigSetting`) |
| `internal/dof/configurator.go` | `LedControl/Setup/Configurator.cs` (toy assignment + effect chain order) |
| `internal/dof/effects.go`, `condition.go`, `alarms.go` | `FX/*`, `PinballSupport/AlarmHandler.cs` |
| `internal/dof/cabinet.go` | `Cab/Toys/Layer/*`, `LWEquivalent`, `General/Curve.cs` |
| `internal/dof/pinonectl.go`, `internal/pinone` | `Cab/Out/PinOne/*`, `OutputControllerCompleteBase` |
| `internal/dof/engine.go` | `Pinball.cs` main loop (single goroutine, input queue + alarms) |
| `internal/linx/*` | behaviour of DOFLinx 10.03 as documented and as observed from its files |

Effect chain order per ini setting (closest to the toy first): leaf (analog value / RGBA layer) →
fade → inner blink → blink → duration → max duration → min duration → extend → wait (delay) →
invert → full range (bool) → condition / element assignment.

## Threading

* **engine goroutine**: owns the table, effects and alarms (exactly DOF's main thread). Everything
  that touches it goes through `Engine.Do`.
* **controller goroutine** (PinOne): snapshots output values, sends changed banks, reconnects.
* **DOFLinx animations**: small goroutines (flasher / button / device timers) that write to
  outputs with their own key; `Output` merges the DOF toy value and all external keys with `max`.

## DOFLinx vs DOF outputs

DOFLinx drives hardware directly (`LINK_xx`, flashers, buttons, RGB) *and* triggers DOF through
`FF_DOF`. Both end on the same PinOne outputs; an `Output` is the max of the DOF toy value and all
DOFLinx sources so they never overwrite each other.

## Where to extend

* another controller: implement `dof.Controller` and add it to `Cabinet`.
* another game/emulator: add a `Profile` in `internal/linx/manager.go` and an event source that
  calls `Runner.Event/Status/Nudge`.
