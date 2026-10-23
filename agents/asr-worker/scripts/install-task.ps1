param([string]$TaskName = "Orbit ASR Worker", [string]$Config = "config.json", [switch]$Gui)
$ErrorActionPreference = "Stop"
$Root = Split-Path $PSScriptRoot -Parent
$ConfigPath = if ([System.IO.Path]::IsPathRooted($Config)) { $Config } else { Join-Path $Root $Config }
$PythonName = if ($Gui) { "pythonw.exe" } else { "python.exe" }
$Python = Join-Path $Root ".venv/Scripts/$PythonName"
if (!(Test-Path $Python)) { throw "Python environment missing; run scripts/install.ps1 first" }
$Module = if ($Gui) { "orbit_asr.gui" } else { "orbit_asr.worker" }
$Arguments = "-m $Module --config `"$ConfigPath`""
if ($Gui) { $Arguments += " --start --minimized" }
$Action = New-ScheduledTaskAction -Execute $Python -Argument $Arguments -WorkingDirectory $Root
$User = [System.Security.Principal.WindowsIdentity]::GetCurrent().Name
$Trigger = New-ScheduledTaskTrigger -AtLogOn -User $User
$Principal = New-ScheduledTaskPrincipal -UserId $User -LogonType Interactive -RunLevel Limited
$Settings = New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -StartWhenAvailable
Register-ScheduledTask -TaskName $TaskName -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings -Description "Orbit durable Qwen ASR worker" -Force
