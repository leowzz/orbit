param([ValidateSet("cpu", "cuda")][string]$Device = "cpu", [string]$TorchIndex = "https://download.pytorch.org/whl/cu128", [switch]$Headless)
$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)
# Never recreate or update an existing environment: it may serve a running worker.
if (Test-Path .venv) {
    throw "An existing .venv was found; installation stopped without changing it. Stop its worker and maintain it explicitly, or install into a separate project directory."
}
$HasPython311 = $false
if (Get-Command py -ErrorAction SilentlyContinue) {
    try {
        & py -3.11 -c "import struct, sys; sys.exit(0 if struct.calcsize('P') == 8 else 1)" 2>$null
        $HasPython311 = ($LASTEXITCODE -eq 0)
    } catch { $HasPython311 = $false }
}
if ($HasPython311) {
    & py -3.11 -m venv .venv
    if ($LASTEXITCODE -ne 0) { throw "Python 3.11 venv creation failed" }
} elseif (Get-Command uv -ErrorAction SilentlyContinue) {
    # uv uses an existing managed 3.11, or downloads it when absent; seed pip.
    & uv venv --python 3.11 --managed-python --seed .venv
    if ($LASTEXITCODE -ne 0) { throw "uv could not create Python 3.11 venv; check network access and uv Python download settings" }
} else {
    throw "Install 64-bit Python 3.11 with the py launcher, or install uv so it can download Python 3.11, then run this script again."
}
$Python = Join-Path $PWD ".venv/Scripts/python.exe"
$Index = if ($Device -eq "cpu") { "https://download.pytorch.org/whl/cpu" } else { $TorchIndex }
& $Python -m pip install --upgrade pip
if ($LASTEXITCODE -ne 0) { throw "pip upgrade failed" }
& $Python -m pip install torch --index-url $Index
if ($LASTEXITCODE -ne 0) { throw "PyTorch installation failed" }
$Extras = if ($Headless) { ".[model]" } else { ".[model,gui]" }
& $Python -m pip install $Extras
if ($LASTEXITCODE -ne 0) { throw "Worker installation failed" }
if (!(Test-Path config.json)) { Copy-Item config.example.json config.json }
& $Python -c "import av, torch; from qwen_asr import Qwen3ASRModel; print('Opus:', av.Codec('opus','r').name, 'CUDA:', torch.cuda.is_available())"
if ($LASTEXITCODE -ne 0) { throw "Dependency smoke failed" }
if (!$Headless) {
    & $Python -c "import PySide6, keyring; print('GUI dependencies available')"
    if ($LASTEXITCODE -ne 0) { throw "GUI dependency smoke failed" }
}
