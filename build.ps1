# Builds the B2S bridge, embeds it, then builds superdof for Windows/Linux/macOS.
$ErrorActionPreference = 'Stop'
dotnet build bridge/B2SBridge -c Release -o bridge/B2SBridge/out
New-Item -ItemType Directory -Force cmd/superdof/bridgedll | Out-Null
Copy-Item bridge/B2SBridge/out/B2SServerDirectOutputPlugin.dll cmd/superdof/bridgedll/ -Force
New-Item -ItemType Directory -Force dist | Out-Null
# superdof.exe is a Windows GUI-subsystem program: started from the Startup folder it opens no
# window (and logs to %ProgramData%\SuperDOF\superdof.log); started from a command prompt it
# attaches to that console. superdof-console.exe is the plain console build.
$targets = @(
  @('windows','amd64','superdof.exe','-s -w -H=windowsgui'),
  @('windows','amd64','superdof-console.exe','-s -w'),
  @('linux','amd64','superdof-linux','-s -w'),
  @('darwin','arm64','superdof-macos','-s -w'))
foreach ($t in $targets) {
  $env:GOOS = $t[0]; $env:GOARCH = $t[1]
  go build -trimpath -ldflags $t[3] -o (Join-Path dist $t[2]) ./cmd/superdof
}
Remove-Item Env:GOOS, Env:GOARCH
