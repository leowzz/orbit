param([string]$Config = "config.json")
$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)
& ./.venv/Scripts/python.exe -m orbit_asr.worker --config $Config
exit $LASTEXITCODE
