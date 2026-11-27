# Builds FlClash for Windows into dist\windows\FlClash\: the agent with the
# icon in the notification area (FlClash.exe), the core, the helper and the
# command line from the Go sources, and the WinUI window under window\.
#
#   powershell -ExecutionPolicy Bypass -File windows\build.ps1 [-Arch x64|arm64] [-SkipGo] [-SkipWindow]
param(
    [ValidateSet('x64', 'arm64')] [string] $Arch = 'x64',
    [switch] $SkipGo,
    [switch] $SkipWindow
)
$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$out = Join-Path $root 'dist\windows\FlClash'
$version = '0.1.0'  # as in macos/Resources/Info.plist
New-Item -ItemType Directory -Force $out | Out-Null
Push-Location $root
try {
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
        $env:GOOS = 'windows'
        $env:GOARCH = if ($Arch -eq 'arm64') { 'arm64' } else { 'amd64' }
        # The agent carries the icon and a manifest; without them it still works.
        go run github.com/tc-hib/go-winres@v0.3.3 simply --icon assets/icon-1024.png --manifest gui `
            --product-name FlClash --file-description FlClash --product-version $version --file-version $version `
            --original-filename FlClash.exe --out cmd/flclash-windows/rsrc
        if ($LASTEXITCODE -ne 0) { Write-Warning 'go-winres failed: FlClash.exe is built without its icon' }
        go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$version" -o "$out\FlClash.exe" ./cmd/flclash-windows
        if ($LASTEXITCODE -ne 0) { throw 'FlClash.exe did not build' }
        go build -tags with_gvisor -trimpath -ldflags '-s -w' -o "$out\flclash-core.exe" ./cmd/flclash-core
        if ($LASTEXITCODE -ne 0) { throw 'flclash-core.exe did not build' }
        go build -tags with_gvisor -trimpath -ldflags '-s -w' -o "$out\flclashctl.exe" ./cmd/flclashctl
        if ($LASTEXITCODE -ne 0) { throw 'flclashctl.exe did not build' }
        go build -trimpath -ldflags '-s -w' -o "$out\flclash-helper.exe" ./cmd/flclash-helper
        if ($LASTEXITCODE -ne 0) { throw 'flclash-helper.exe did not build' }
    }

    if (-not $SkipWindow) {
        $window = Join-Path $out 'window'
        if (Test-Path $window) { Remove-Item -Recurse -Force $window }
        dotnet publish windows\FlClash.Window\FlClash.Window.csproj -c Release -r "win-$Arch" -p:Platform=$Arch -p:Version=$version -o $window --nologo
        if ($LASTEXITCODE -ne 0) { throw 'the window did not build' }
    }
}
finally {
    Pop-Location
}
Write-Host "built $out"
