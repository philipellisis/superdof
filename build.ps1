# Builds the B2S bridge, embeds it, then builds superdof for Windows/Linux/macOS.
$ErrorActionPreference = 'Stop'
dotnet build bridge/B2SBridge -c Release -o bridge/B2SBridge/out
New-Item -ItemType Directory -Force cmd/superdof/bridgedll | Out-Null
Copy-Item bridge/B2SBridge/out/B2SServerDirectOutputPlugin.dll cmd/superdof/bridgedll/ -Force
New-Item -ItemType Directory -Force dist | Out-Null
$targets = @(@('windows','amd64','superdof.exe'), @('linux','amd64','superdof-linux'), @('darwin','arm64','superdof-macos'))
foreach ($t in $targets) {
  $env:GOOS = $t[0]; $env:GOARCH = $t[1]
  go build -trimpath -ldflags '-s -w' -o (Join-Path dist $t[2]) ./cmd/superdof
}
Remove-Item Env:GOOS, Env:GOARCH
