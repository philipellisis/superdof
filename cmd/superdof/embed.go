package main

import _ "embed"

// bridgeDLL is the B2S server plugin shim built from bridge/B2SBridge
// (build.ps1 copies it to cmd/superdof/bridgedll before building).
//
//go:embed bridgedll/B2SServerDirectOutputPlugin.dll
var bridgeDLL []byte
