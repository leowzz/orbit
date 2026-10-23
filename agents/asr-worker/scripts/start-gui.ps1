param([string]$Config = "config.json", [switch]$StartWorker, [switch]$Minimized)
$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
Set-Location $Root
$Python = Join-Path $Root ".venv/Scripts/pythonw.exe"
if (!(Test-Path $Python)) { throw "Python GUI environment missing; run scripts/install.ps1 first" }
$ConfigPath = [System.IO.Path]::GetFullPath($Config)
$Arguments = @("-m", "orbit_asr.gui", "--config", "`"$ConfigPath`"")
if ($StartWorker) { $Arguments += "--start" }
if ($Minimized) { $Arguments += "--minimized" }
Start-Process -FilePath $Python -ArgumentList $Arguments -WorkingDirectory $Root
