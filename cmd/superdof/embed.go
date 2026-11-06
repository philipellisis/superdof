package main

import _ "embed"

// bridgeDLL is the B2S server plugin shim built from bridge/B2SBridge
// (build.ps1 copies it to cmd/superdof/bridgedll before building).
//
//go:embed bridgedll/B2SServerDirectOutputPlugin.dll
var bridgeDLL []byte

// triggerDLL is the unmodified DOFLinxTrigger.dll from the DOFLinx 10.03
// download (by DDH69). Pinball FX / M / Infected Mushroom load it as their
// cabinet plugin; superdof copies it next to the game executable.
//
//go:embed triggerdll/DOFLinxTrigger.dll
var triggerDLL []byte

// fpScript is DOFLinx.vbs (DOFLinx 10.03, by DDH69): Future Pinball tables load
// it with LoadExternalScript from the Future Pinball Scripts folder.
//
//go:embed fpscript/DOFLinx.vbs
var fpScript []byte

// comDLL is the DirectOutput.ComObject replacement built from bridge/ComShim.
//
//go:embed comdll/DirectOutputComObject.dll
var comDLL []byte
