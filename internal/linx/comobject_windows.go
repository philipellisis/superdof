//go:build windows

package linx

import "path/filepath"

// DirectOutput registers its own COM object under HKCU too, so the registration
// of the replacement overwrites it. The original keys are exported to
// SD_BACKUP first (once, never over a backup, never an export of our own keys)
// and importing them again restores DirectOutput.

const comPrelude = `
$g = '{A23BFDBC-9A8A-46C0-8672-60F23D54FFB6}'
$roots = @('HKCU\Software\Classes', 'HKCU\Software\Classes\WOW6432Node')
function IsOurs($root) {
  $p = "Registry::$root\CLSID\$g\InprocServer32"
  if (-not (Test-Path $p)) { return $false }
  $cb = (Get-ItemProperty $p -ErrorAction SilentlyContinue).CodeBase
  return ($cb -and $cb -like '*SuperDOF*ComObject*')
}
`

const comScript = comPrelude + `
$ErrorActionPreference = 'Stop'
New-Item -Force -ItemType Directory -Path $env:SD_BACKUP | Out-Null
$i = 0
foreach ($root in $roots) {
  $i++
  if ((Test-Path "Registry::$root\CLSID\$g") -and -not (IsOurs $root)) {
    $f = Join-Path $env:SD_BACKUP "clsid$i.reg"; $f2 = Join-Path $env:SD_BACKUP "progid$i.reg"
    if (-not (Test-Path $f)) { reg.exe export "$root\CLSID\$g" $f /y | Out-Null }
    if (-not (Test-Path $f)) { throw "could not back up the existing DirectOutput COM registration; nothing was changed" }
    if ((Test-Path "Registry::$root\DirectOutput.ComObject") -and -not (Test-Path $f2)) { reg.exe export "$root\DirectOutput.ComObject" $f2 /y | Out-Null }
  }
}
$asm = 'DirectOutputComObject, Version=3.2.9084.21421, Culture=neutral, PublicKeyToken=null'
$cb = ([Uri]$env:SD_DLL).AbsoluteUri
foreach ($root in $roots) {
  $k = "Registry::$root\CLSID\$g"
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -Path $k
  New-Item -Force -Path $k | Out-Null
  Set-ItemProperty -Path $k -Name '(default)' -Value 'DirectOutputCom.ComObject'
  New-Item -Force -Path "$k\ProgId" | Out-Null
  Set-ItemProperty -Path "$k\ProgId" -Name '(default)' -Value 'DirectOutput.ComObject'
  foreach ($sub in 'InprocServer32', 'InprocServer32\3.2.9084.21421') {
    New-Item -Force -Path "$k\$sub" | Out-Null
    Set-ItemProperty -Path "$k\$sub" -Name 'Class' -Value 'DirectOutputCom.ComObject'
    Set-ItemProperty -Path "$k\$sub" -Name 'Assembly' -Value $asm
    Set-ItemProperty -Path "$k\$sub" -Name 'RuntimeVersion' -Value 'v4.0.30319'
    Set-ItemProperty -Path "$k\$sub" -Name 'CodeBase' -Value $cb
  }
  Set-ItemProperty -Path "$k\InprocServer32" -Name '(default)' -Value 'mscoree.dll'
  Set-ItemProperty -Path "$k\InprocServer32" -Name 'ThreadingModel' -Value 'Both'
  $p = "Registry::$root\DirectOutput.ComObject"
  Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -Path $p
  New-Item -Force -Path "$p\CLSID" | Out-Null
  Set-ItemProperty -Path $p -Name '(default)' -Value 'DirectOutputCom.ComObject'
  Set-ItemProperty -Path "$p\CLSID" -Name '(default)' -Value $g
}
`

const comRemoveScript = comPrelude + `
foreach ($root in $roots) {
  if (IsOurs $root) {
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -Path "Registry::$root\CLSID\$g"
    Remove-Item -Recurse -Force -ErrorAction SilentlyContinue -Path "Registry::$root\DirectOutput.ComObject"
  }
}
Get-ChildItem -ErrorAction SilentlyContinue -Path $env:SD_BACKUP -Filter *.reg | ForEach-Object {
  reg.exe import $_.FullName | Out-Null
  Remove-Item -Force $_.FullName
}
`

func comBackupDir() string { return filepath.Join(ComObjectDir(), "backup") }

func registerComObject(dll string) error {
	_, err := powershell(comScript, "SD_DLL="+dll, "SD_BACKUP="+comBackupDir())
	return err
}

func unregisterComObject() error {
	_, err := powershell(comRemoveScript, "SD_BACKUP="+comBackupDir())
	return err
}
