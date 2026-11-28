# Builds FlClash for Windows into dist\windows\FlClash\: the agent with the
# icon in the notification area (FlClash.exe), the core, the helper and the
# command line from the Go sources, and the window under window\: WPF on the
# .NET Framework that Windows carries, the same build for x64 and Arm64.
#
#   powershell -ExecutionPolicy Bypass -File windows\build.ps1 [-Arch x64|arm64] [-Version X.Y.Z] [-Channel release] [-SkipGo] [-SkipWindow]
#
# -Channel release is for the release workflow: only such a copy updates itself.
#
# The version comes from the last v* tag, as in the Makefile: v0.2.0 builds
# 0.2.0, a commit after it 0.2.0-3-gabc1234. File versions take the numbers only.
param(
    [ValidateSet('x64', 'arm64')] [string] $Arch = 'x64',
    [string] $Version = '',
    [string] $Channel = '',
    [switch] $SkipGo,
    [switch] $SkipWindow
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root 'dist\windows\FlClash'
New-Item -ItemType Directory -Force $out | Out-Null
Push-Location $root
try {
    if (-not $Version) {
        # Windows PowerShell turns git's complaint about no tag into an error.
        try { $Version = (git describe --tags --match 'v[0-9]*' 2>$null) -replace '^v', '' } catch { $Version = '' }
        if (-not $Version) { $Version = '0.0.0-dev' }
    }
    $short = ($Version -split '[-+]')[0]
    # The icon of the window and of the files, from the one picture all desktops use.
    $icon = Join-Path $root 'windows\FlClash.ico'
    $png = Join-Path $root 'assets\icon-1024.png'
    if (-not (Test-Path $icon) -or (Get-Item $icon).LastWriteTime -lt (Get-Item $png).LastWriteTime) {
        Add-Type -AssemblyName System.Drawing
        $source = [System.Drawing.Image]::FromFile($png)
        $sizes = 16, 20, 24, 32, 40, 48, 64, 256
        $images = foreach ($s in $sizes) {
            $bitmap = New-Object System.Drawing.Bitmap $s, $s
            $g = [System.Drawing.Graphics]::FromImage($bitmap)
            $g.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
            $g.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
            $g.DrawImage($source, 0, 0, $s, $s)
            $g.Dispose()
            $stream = New-Object System.IO.MemoryStream
            $bitmap.Save($stream, [System.Drawing.Imaging.ImageFormat]::Png)
            $bitmap.Dispose()
            , $stream.ToArray()
        }
        $source.Dispose()
        $file = New-Object System.IO.MemoryStream
        $w = New-Object System.IO.BinaryWriter $file
        $w.Write([uint16]0); $w.Write([uint16]1); $w.Write([uint16]$sizes.Count)
        $offset = 6 + 16 * $sizes.Count
        for ($i = 0; $i -lt $sizes.Count; $i++) {
            $side = $sizes[$i] % 256
            $w.Write([byte]$side); $w.Write([byte]$side); $w.Write([byte]0); $w.Write([byte]0)
            $w.Write([uint16]1); $w.Write([uint16]32)
            $w.Write([uint32]$images[$i].Length); $w.Write([uint32]$offset)
            $offset += $images[$i].Length
        }
        foreach ($img in $images) { $w.Write($img) }
        $w.Flush()
        [System.IO.File]::WriteAllBytes($icon, $file.ToArray())
    }

    if (-not $SkipGo) {
        # The agent carries the icon, a manifest and its version. go-winres
        # runs here, so it is built for this machine, before GOARCH is set;
        # it writes the resources for both architectures.
        Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
        go run github.com/tc-hib/go-winres@v0.3.3 simply --icon assets/icon-1024.png --manifest gui `
            --product-name FlClash --file-description FlClash --product-version $short --file-version $short `
            --original-filename FlClash.exe --arch amd64,arm64 --out cmd/flclash-windows/rsrc
        if ($LASTEXITCODE -ne 0) { throw 'go-winres failed' }
        $env:GOOS = 'windows'
        $env:GOARCH = if ($Arch -eq 'arm64') { 'arm64' } else { 'amd64' }
        $ldflags = "-s -w -X github.com/oviron/flclash-desktop/internal/build.Version=$Version -X github.com/oviron/flclash-desktop/internal/build.Channel=$Channel"
        go build -trimpath -ldflags "$ldflags -H windowsgui" -o "$out\FlClash.exe" ./cmd/flclash-windows
        if ($LASTEXITCODE -ne 0) { throw 'FlClash.exe did not build' }
        # The same tags as CORE_TAGS in the Makefile.
        go build -tags no_easytier,no_zerotier -trimpath -ldflags $ldflags -o "$out\flclash-core.exe" ./cmd/flclash-core
        if ($LASTEXITCODE -ne 0) { throw 'flclash-core.exe did not build' }
        go build -trimpath -ldflags $ldflags -o "$out\flclashctl.exe" ./cmd/flclashctl
        if ($LASTEXITCODE -ne 0) { throw 'flclashctl.exe did not build' }
        go build -trimpath -ldflags $ldflags -o "$out\flclash-helper.exe" ./cmd/flclash-helper
        if ($LASTEXITCODE -ne 0) { throw 'flclash-helper.exe did not build' }
    }

    if (-not $SkipWindow) {
        $window = Join-Path $out 'window'
        if (Test-Path $window) { Remove-Item -Recurse -Force $window }
        dotnet build windows\FlClash.Window\FlClash.Window.csproj -c Release -p:Version=$short -p:InformationalVersion=$Version -o $window --nologo
        if ($LASTEXITCODE -ne 0) { throw 'the window did not build' }
    }
}
finally {
    Pop-Location
}
Write-Host "built $out"
