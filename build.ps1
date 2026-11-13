# Windows build for Aida64SD (the Makefile is for the Linux cross-compile setup).
#
#   powershell -ExecutionPolicy Bypass -File .\build.ps1            build only
#   powershell -ExecutionPolicy Bypass -File .\build.ps1 -Install   build, then install into Stream Deck
#
# Compiles the plugin host (build\aida64.exe) from source. The sensor bridge
# (build\aida64-bridge.exe) reads AIDA64's shared memory through cgo, so it
# needs a C compiler: if one is on the PATH (for example MSYS2 mingw-w64 gcc)
# the bridge is compiled too; otherwise an existing build\aida64-bridge.exe is
# used (copy it from a release, or build it with `make bridge` on Linux/WSL).
# Needs Go (winget install GoLang.Go).
param([switch]$Install)

Set-Location $PSScriptRoot
$plugin = "com.poizenjam.aida64sd.sdPlugin"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "Go not found. Run: winget install GoLang.Go  then open a new PowerShell." -ForegroundColor Red
    exit 1
}

New-Item -ItemType Directory -Force build | Out-Null
# -trimpath keeps local source paths (and so the Windows user name) out of the
# exes; -s -w drops the symbol table and debug info, roughly halving the size.
$goFlags = @("-trimpath", "-ldflags=-s -w")
Write-Host "Building build\aida64.exe ..."
go build @goFlags -o build\aida64.exe .\cmd\aida64
if ($LASTEXITCODE -ne 0) {
    Write-Host "go build failed; nothing was installed." -ForegroundColor Red
    exit 1
}

if (Get-Command gcc -ErrorAction SilentlyContinue) {
    Write-Host "Building build\aida64-bridge.exe (cgo) ..."
    $env:CGO_ENABLED = "1"
    go build @goFlags -o build\aida64-bridge.exe .\cmd\aida64-bridge
    $bridgeExit = $LASTEXITCODE
    Remove-Item Env:\CGO_ENABLED
    if ($bridgeExit -ne 0) {
        Write-Host "Bridge build failed; nothing was installed." -ForegroundColor Red
        exit 1
    }
} elseif (-not (Test-Path build\aida64-bridge.exe)) {
    Write-Host "No C compiler found and no build\aida64-bridge.exe present." -ForegroundColor Red
    Write-Host "Either install MSYS2 with mingw-w64 gcc and put it on the PATH, or copy"
    Write-Host "aida64-bridge.exe from a release (or a Linux/WSL 'make bridge') into build\."
    exit 1
} else {
    Write-Host "No C compiler on the PATH; using the existing build\aida64-bridge.exe."
}

# Complete plugin folder: PI files + manifest + both exes.
Remove-Item dist -Recurse -Force -ErrorAction SilentlyContinue
New-Item -ItemType Directory dist | Out-Null
Copy-Item $plugin dist -Recurse
Copy-Item build\aida64.exe, build\aida64-bridge.exe "dist\$plugin"
Write-Host "Plugin folder ready: dist\$plugin" -ForegroundColor Green

if (-not $Install) { exit 0 }

# Stream Deck must not be running, or the exes are locked.
Get-Process StreamDeck -ErrorAction SilentlyContinue | Stop-Process
Get-Process StreamDeck -ErrorAction SilentlyContinue | Wait-Process -Timeout 15
Start-Sleep 2

$plugins = "$env:APPDATA\Elgato\StreamDeck\Plugins"
Remove-Item "$plugins\$plugin" -Recurse -Force -ErrorAction SilentlyContinue
if (Test-Path "$plugins\$plugin") {
    Write-Host "Could not remove the installed plugin folder (still in use?). Close Stream Deck and re-run." -ForegroundColor Red
    exit 1
}
Copy-Item "dist\$plugin" $plugins -Recurse
Start-Process "$env:ProgramFiles\Elgato\StreamDeck\StreamDeck.exe"
$version = (Get-Content "$plugin\manifest.json" -Raw | ConvertFrom-Json).Version
Write-Host "Installed $version. A Settings key's Property Inspector shows the version under Status." -ForegroundColor Green
