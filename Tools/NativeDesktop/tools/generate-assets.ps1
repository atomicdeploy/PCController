param(
    [int]$FrameCount = 24
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'terminal.ps1')

$projectRoot = Split-Path -Parent $PSScriptRoot
$assetRoot = Join-Path $projectRoot 'assets'
$frameRoot = Join-Path $assetRoot 'animation'
$iconRoot = Join-Path $assetRoot 'icons'
$temporaryRoot = Join-Path $assetRoot ('.pccontroller-desktop-assets-' + [Guid]::NewGuid().ToString('N'))
$tempSvg = Join-Path $temporaryRoot 'frame.svg'
$tempBadge = Join-Path $temporaryRoot 'badge.png'
$iconSizes = @(256, 128, 112, 96, 80, 72, 64, 56, 48, 40, 36, 32, 28, 24, 20, 16)

New-Item -ItemType Directory -Force -Path $frameRoot, $iconRoot, $temporaryRoot | Out-Null

$magick = Get-Command magick -ErrorAction Stop

function Write-Utf8NoBom {
    param(
        [Parameter(Mandatory)][string]$LiteralPath,
        [Parameter(Mandatory)][string]$Value
    )

    $encoding = New-Object Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($LiteralPath, $Value, $encoding)
}

function Invoke-Magick {
    param([Parameter(Mandatory)][string[]]$Arguments)

    & $magick.Source @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "ImageMagick failed with exit code $LASTEXITCODE."
    }
}

function Write-SvgPng {
    param(
        [Parameter(Mandatory)][string]$SvgPath,
        [Parameter(Mandatory)][string]$OutputPath,
        [Parameter(Mandatory)][int]$Size
    )

    Invoke-Magick -Arguments @(
        '-background', 'none',
        '-density', '384',
        $SvgPath,
        '-colorspace', 'sRGB',
        '-alpha', 'on',
        '-filter', 'Lanczos',
        '-define', 'filter:blur=0.88',
        '-resize', "${Size}x${Size}!",
        '-depth', '8',
        '-strip',
        '-define', 'png:color-type=6',
        $OutputPath
    )
}

function Write-RasterPng {
    param(
        [Parameter(Mandatory)][string]$SourcePath,
        [Parameter(Mandatory)][string]$OutputPath,
        [Parameter(Mandatory)][int]$Size
    )

    Invoke-Magick -Arguments @(
        $SourcePath,
        '-colorspace', 'sRGB',
        '-alpha', 'on',
        '-filter', 'Lanczos',
        '-define', 'filter:blur=0.88',
        '-resize', "${Size}x${Size}!",
        '-depth', '8',
        '-strip',
        '-define', 'png:color-type=6',
        $OutputPath
    )
}

function Assert-IconPack {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][int[]]$ExpectedSizes
    )

    $bytes = [IO.File]::ReadAllBytes($Path)
    if ($bytes.Length -lt (6 + 16 * $ExpectedSizes.Count)) {
        throw "Generated icon pack is truncated: $Path"
    }
    if ([BitConverter]::ToUInt16($bytes, 0) -ne 0 -or
        [BitConverter]::ToUInt16($bytes, 2) -ne 1) {
        throw "Generated file is not an ICO pack: $Path"
    }
    $count = [BitConverter]::ToUInt16($bytes, 4)
    if ($count -ne $ExpectedSizes.Count) {
        throw "Generated ICO has $count frames; expected $($ExpectedSizes.Count): $Path"
    }

    $actualSizes = [Collections.Generic.HashSet[int]]::new()
    for ($index = 0; $index -lt $count; $index++) {
        $entry = 6 + $index * 16
        $width = [int]$bytes[$entry]
        $height = [int]$bytes[$entry + 1]
        if ($width -eq 0) { $width = 256 }
        if ($height -eq 0) { $height = 256 }
        if ($width -ne $height -or -not $actualSizes.Add($width)) {
            throw "Generated ICO contains a duplicate or non-square frame: $Path"
        }
        if ([BitConverter]::ToUInt16($bytes, $entry + 6) -ne 32) {
            throw "Generated ICO frame $width is not 32-bit: $Path"
        }
        $length = [BitConverter]::ToUInt32($bytes, $entry + 8)
        $offset = [BitConverter]::ToUInt32($bytes, $entry + 12)
        if ($length -eq 0 -or
            [uint64]$offset + [uint64]$length -gt [uint64]$bytes.Length) {
            throw "Generated ICO frame $width has invalid bounds: $Path"
        }
    }
    foreach ($size in $ExpectedSizes) {
        if (-not $actualSizes.Contains($size)) {
            throw "Generated ICO is missing its ${size}px frame: $Path"
        }
    }
}

function Write-IconPack {
    param(
        [Parameter(Mandatory)][string]$SvgPath,
        [Parameter(Mandatory)][string]$OutputPath,
        [string]$SmallSvgPath = ''
    )

    $packRoot = Join-Path $temporaryRoot ([IO.Path]::GetFileNameWithoutExtension($OutputPath))
    New-Item -ItemType Directory -Force -Path $packRoot | Out-Null
    $frames = [Collections.Generic.List[string]]::new()
    foreach ($size in $iconSizes) {
        $source = $SvgPath
        if ($size -le 32 -and -not [string]::IsNullOrWhiteSpace($SmallSvgPath)) {
            $source = $SmallSvgPath
        }
        $frame = Join-Path $packRoot ('icon-{0:D3}.png' -f $size)
        Write-SvgPng -SvgPath $source -OutputPath $frame -Size $size
        $frames.Add($frame)
    }
    $temporaryIcon = Join-Path $packRoot 'pack.ico'
    Invoke-Magick -Arguments (@($frames) + @($temporaryIcon))
    Assert-IconPack -Path $temporaryIcon -ExpectedSizes $iconSizes
    Move-Item -LiteralPath $temporaryIcon -Destination $OutputPath -Force
}

function Write-RasterIconPack {
    param(
        [Parameter(Mandatory)][string]$SourcePath,
        [Parameter(Mandatory)][string]$OutputPath
    )

    $packRoot = Join-Path $temporaryRoot (
        [IO.Path]::GetFileNameWithoutExtension($OutputPath) + '-raster'
    )
    New-Item -ItemType Directory -Force -Path $packRoot | Out-Null
    $frames = [Collections.Generic.List[string]]::new()
    foreach ($size in $iconSizes) {
        $frame = Join-Path $packRoot ('icon-{0:D3}.png' -f $size)
        Write-RasterPng -SourcePath $SourcePath -OutputPath $frame -Size $size
        $frames.Add($frame)
    }
    $temporaryIcon = Join-Path $packRoot 'pack.ico'
    Invoke-Magick -Arguments (@($frames) + @($temporaryIcon))
    Assert-IconPack -Path $temporaryIcon -ExpectedSizes $iconSizes
    Move-Item -LiteralPath $temporaryIcon -Destination $OutputPath -Force
}

function Mix-Color {
    param([int[]]$From, [int[]]$To, [double]$Amount)
    $r = [int][Math]::Round($From[0] + ($To[0] - $From[0]) * $Amount)
    $g = [int][Math]::Round($From[1] + ($To[1] - $From[1]) * $Amount)
    $b = [int][Math]::Round($From[2] + ($To[2] - $From[2]) * $Amount)
    return '#{0:X2}{1:X2}{2:X2}' -f $r, $g, $b
}

function Ease-Out-Cubic([double]$x) {
    return 1.0 - [Math]::Pow(1.0 - $x, 3)
}

function New-DeviceSvg {
    param([double]$Progress)

    $p = Ease-Out-Cubic $Progress
    $glassTop = Mix-Color @(49, 91, 127) @(255, 250, 213) $p
    $glassBottom = Mix-Color @(13, 31, 51) @(255, 188, 70) $p
    $core = Mix-Color @(69, 148, 197) @(255, 244, 169) $p
    $haloOpacity = [Math]::Round(0.10 + 0.50 * $p, 3)
    $innerOpacity = [Math]::Round(0.12 + 0.72 * $p, 3)
    $rayOpacity = [Math]::Round([Math]::Max(0, ($p - 0.18) / 0.82) * 0.88, 3)
    $filamentOpacity = [Math]::Round(0.20 + 0.80 * $p, 3)
    $bounce = [Math]::Round([Math]::Sin($Progress * [Math]::PI) * 4.0, 2)
    $rayScale = [Math]::Round(0.86 + 0.14 * $p, 3)

    return @"
<svg xmlns="http://www.w3.org/2000/svg" width="512" height="512" viewBox="0 0 512 512">
  <defs>
    <radialGradient id="halo" cx="50%" cy="43%" r="52%">
      <stop offset="0" stop-color="#FFECA0" stop-opacity="$haloOpacity"/>
      <stop offset=".52" stop-color="#39BDF8" stop-opacity="$([Math]::Round($haloOpacity * .45, 3))"/>
      <stop offset="1" stop-color="#118DD1" stop-opacity="0"/>
    </radialGradient>
    <linearGradient id="glass" x1="0" y1="0" x2="0.78" y2="1">
      <stop offset="0" stop-color="$glassTop"/>
      <stop offset=".56" stop-color="$core"/>
      <stop offset="1" stop-color="$glassBottom"/>
    </linearGradient>
    <linearGradient id="base" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0" stop-color="#7D8C9B"/>
      <stop offset=".45" stop-color="#465462"/>
      <stop offset="1" stop-color="#202B36"/>
    </linearGradient>
    <filter id="softGlow" x="-80%" y="-80%" width="260%" height="260%">
      <feGaussianBlur stdDeviation="$([Math]::Round(11 + 9 * $p, 2))"/>
    </filter>
  </defs>

  <circle cx="256" cy="222" r="205" fill="url(#halo)"/>

  <g transform="translate(256 208) scale($rayScale) translate(-256 -208)">
    <path d="M256 15v35" opacity="$rayOpacity" stroke="#FFD86A" stroke-width="13" stroke-linecap="round"/>
    <path d="M124 57l25 31" opacity="$rayOpacity" stroke="#FFD86A" stroke-width="13" stroke-linecap="round"/>
    <path d="M50 173l41 8" opacity="$rayOpacity" stroke="#FFD86A" stroke-width="13" stroke-linecap="round"/>
    <path d="M388 57l-25 31" opacity="$rayOpacity" stroke="#FFD86A" stroke-width="13" stroke-linecap="round"/>
    <path d="M462 173l-41 8" opacity="$rayOpacity" stroke="#FFD86A" stroke-width="13" stroke-linecap="round"/>
    <path d="M70 305l37-17" opacity="$rayOpacity" stroke="#FFD86A" stroke-width="13" stroke-linecap="round"/>
    <path d="M442 305l-37-17" opacity="$rayOpacity" stroke="#FFD86A" stroke-width="13" stroke-linecap="round"/>
  </g>

  <g transform="translate(0 -$bounce)">
    <path d="M256 66C177 66 120 126 120 202c0 55 29 89 57 117
             17 17 24 35 25 57h108c1-22 8-40 25-57 28-28 57-62 57-117
             0-76-57-136-136-136z"
          fill="#FFD86A" opacity="$innerOpacity" filter="url(#softGlow)"/>
    <path d="M256 66C177 66 120 126 120 202c0 55 29 89 57 117
             17 17 24 35 25 57h108c1-22 8-40 25-57 28-28 57-62 57-117
             0-76-57-136-136-136z"
          fill="url(#glass)" stroke="#7ED9FF" stroke-width="8" stroke-opacity=".82"/>
    <path d="M185 151c15-31 42-51 77-55" fill="none" stroke="#FFFFFF"
          stroke-width="14" stroke-linecap="round" opacity="$([Math]::Round(.24 + .48 * $p, 3))"/>
    <path d="M218 281l38-49 38 49M256 232v98" fill="none" stroke="#FFF5C5"
          stroke-width="10" stroke-linecap="round" stroke-linejoin="round"
          opacity="$filamentOpacity"/>

    <path d="M197 369h118v30c0 10-8 18-18 18h-82c-10 0-18-8-18-18z"
          fill="url(#base)" stroke="#9FB0BF" stroke-width="5"/>
    <path d="M205 390h102M209 407h94" fill="none" stroke="#C2CDD6"
          stroke-width="6" stroke-linecap="round" opacity=".78"/>
    <path d="M222 418h68v10c0 22-15 38-34 38s-34-16-34-38z"
          fill="#26323E"/>
    <ellipse cx="256" cy="452" rx="25" ry="8" fill="#121A22" opacity=".72"/>
  </g>
</svg>
"@
}

function New-DeviceSmallIconSvg {
    param([double]$Progress)

    $p = Ease-Out-Cubic $Progress
    $glassTop = Mix-Color @(49, 91, 127) @(255, 250, 213) $p
    $glassBottom = Mix-Color @(13, 31, 51) @(255, 188, 70) $p
    $filament = Mix-Color @(126, 217, 255) @(255, 246, 181) $p
    $rayOpacity = [Math]::Round([Math]::Max(0.18, $p) * 0.92, 3)

    return @"
<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
  <defs>
    <linearGradient id="glass" x1="0" y1="0" x2=".8" y2="1">
      <stop offset="0" stop-color="$glassTop"/>
      <stop offset="1" stop-color="$glassBottom"/>
    </linearGradient>
  </defs>
  <g fill="none" stroke="#FFD86A" stroke-width="2.8" stroke-linecap="round"
     opacity="$rayOpacity">
    <path d="M32 2v5M14 8l4 4M50 8l-4 4M5 25l6 1M59 25l-6 1"/>
  </g>
  <path d="M32 9C21 9 13 17 13 28c0 8 4 13 8 17 2 2 3 4 3 7h16
           c0-3 1-5 3-7 4-4 8-9 8-17 0-11-8-19-19-19z"
        fill="url(#glass)" stroke="#8DE1FF" stroke-width="2.5" stroke-linejoin="round"/>
  <path d="M27 37l5-7 5 7M32 30v13" fill="none" stroke="$filament"
        stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round"/>
  <path d="M24 50h16v5c0 2-2 3-4 3h-8c-2 0-4-1-4-3z"
        fill="#43515E" stroke="#AFC0CE" stroke-width="1.8"/>
  <path d="M27 59h10c-1 3-3 4-5 4s-4-1-5-4z" fill="#1B2732"/>
</svg>
"@
}

function New-MenuIconSvg {
    param(
        [Parameter(Mandatory)][string]$Accent,
        [Parameter(Mandatory)][string]$Body,
        [switch]$Small
    )

    if ($Small) {
        return @"
<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
  <defs>
    <linearGradient id="tile" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#1B425B"/>
      <stop offset="1" stop-color="#071722"/>
    </linearGradient>
  </defs>
  <rect x="4" y="4" width="56" height="56" rx="16" fill="url(#tile)"
        stroke="$Accent" stroke-width="3.2"/>
  <g fill="none" stroke="$Accent" stroke-width="4.8" stroke-linecap="round"
     stroke-linejoin="round">
    $Body
  </g>
</svg>
"@
    }

    return @"
<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64">
  <defs>
    <linearGradient id="tile" x1="0" y1="0" x2="1" y2="1">
      <stop offset="0" stop-color="#17354A"/>
      <stop offset="1" stop-color="#071722"/>
    </linearGradient>
    <filter id="glow" x="-50%" y="-50%" width="200%" height="200%">
      <feGaussianBlur stdDeviation="2.2"/>
    </filter>
  </defs>
  <rect x="5" y="5" width="54" height="54" rx="17" fill="url(#tile)"
        stroke="$Accent" stroke-width="2.4"/>
  <path d="M17 15c8-7 22-8 31-1" fill="none" stroke="#FFFFFF"
        stroke-width="2.5" stroke-linecap="round" opacity=".28"/>
  <circle cx="32" cy="34" r="17" fill="$Accent" opacity=".13" filter="url(#glow)"/>
  <g fill="none" stroke="$Accent" stroke-width="4" stroke-linecap="round"
     stroke-linejoin="round">
    $Body
  </g>
</svg>
"@
}

function New-MenuIcon {
    param(
        [string]$Stem,
        [string]$Accent,
        [string]$Body
    )

    $fullSvg = Join-Path $iconRoot "$Stem.svg"
    $smallSvg = Join-Path $iconRoot "$Stem-small.svg"
    Write-Utf8NoBom -LiteralPath $fullSvg `
        -Value (New-MenuIconSvg -Accent $Accent -Body $Body)
    Write-Utf8NoBom -LiteralPath $smallSvg `
        -Value (New-MenuIconSvg -Accent $Accent -Body $Body -Small)
    Write-SvgPng -SvgPath $fullSvg -OutputPath (Join-Path $iconRoot "$Stem.png") -Size 64
    # Only native title/taskbar consumers need multi-resolution menu packs.
    # Tray/menu artwork uses the shared PNG master, so generating ICO copies
    # for every menu action would add unused megabytes to the source tree.
    if ($Stem -in @('menu-settings', 'menu-diagnostics', 'menu-reconnect')) {
        Write-IconPack `
            -SvgPath $fullSvg `
            -SmallSvgPath $smallSvg `
            -OutputPath (Join-Path $iconRoot "$Stem.ico")
    }
}

try {
    Get-ChildItem -LiteralPath $frameRoot -Filter 'device_*.png' -File |
        Remove-Item -Force
    foreach ($unusedPack in @(
        'menu-pin.ico',
        'menu-startup.ico',
        'menu-window-show.ico',
        'menu-window-hide.ico',
        'menu-exit.ico'
    )) {
        Remove-Item -LiteralPath (Join-Path $iconRoot $unusedPack) `
            -Force -ErrorAction SilentlyContinue
    }

    for ($i = 0; $i -lt $FrameCount; $i++) {
        $progress = if ($FrameCount -le 1) { 1.0 } else { $i / ($FrameCount - 1.0) }
        Write-Utf8NoBom -LiteralPath $tempSvg -Value (New-DeviceSvg $progress)
        $frameName = 'device_{0:D3}.png' -f $i
        $framePath = Join-Path $frameRoot $frameName
        if ($i -eq 0) {
            Copy-Item -LiteralPath $tempSvg -Destination (Join-Path $assetRoot 'device-off.svg') -Force
        }
        if ($i -eq ($FrameCount - 1)) {
            # oksvg renders the authored paths directly but does not reliably
            # preserve a redundant identity transform on a group. Remove that
            # transform from the steady-state SVG so every ray remains vector.
            $steadySvg = (Get-Content -LiteralPath $tempSvg -Raw).Replace(
                '<g transform="translate(256 208) scale(1) translate(-256 -208)">',
                '<g>'
            )
            Write-Utf8NoBom `
                -LiteralPath (Join-Path $assetRoot 'device-on.svg') `
                -Value $steadySvg
        }
        Write-SvgPng -SvgPath $tempSvg -OutputPath $framePath -Size 512
    }

    Copy-Item -LiteralPath $tempSvg -Destination (Join-Path $assetRoot 'device-transition.svg') -Force
    Copy-Item -LiteralPath (Join-Path $frameRoot ('device_{0:D3}.png' -f 0)) -Destination (Join-Path $iconRoot 'device-off.png') -Force
    Copy-Item -LiteralPath (Join-Path $frameRoot ('device_{0:D3}.png' -f ($FrameCount - 1))) -Destination (Join-Path $iconRoot 'device-on.png') -Force

    $appSvg = Join-Path $iconRoot 'app.svg'
    $appSmallSvg = Join-Path $iconRoot 'app-small.svg'
    Write-Utf8NoBom -LiteralPath $appSvg -Value (New-DeviceSvg 0.42)
    Write-Utf8NoBom -LiteralPath $appSmallSvg `
        -Value (New-DeviceSmallIconSvg 0.42)
    Write-SvgPng -SvgPath $appSvg -OutputPath (Join-Path $iconRoot 'app.png') -Size 512
    Write-IconPack `
        -SvgPath $appSvg `
        -SmallSvgPath $appSmallSvg `
        -OutputPath (Join-Path $assetRoot 'PCControllerDesktop.ico')

    Invoke-Magick -Arguments @(
        '-background', 'none', '-density', '384', $appSvg,
        '-resize', '256x256!', '-colorspace', 'Gray',
        '-fill', '#7B8794', '-colorize', '35',
        '-depth', '8', '-strip', '-define', 'png:color-type=6',
        (Join-Path $iconRoot 'device-unknown.png')
    )
    Invoke-Magick -Arguments @(
        '-size', '256x256', 'xc:none',
        '-fill', '#0A2434', '-stroke', '#A9ECFF', '-strokewidth', '5',
        '-draw', 'circle 205,205 237,205',
        '-fill', '#A9ECFF', '-stroke', 'none',
        '-draw', 'polygon 190,205 202,193 202,200 220,200 220,210 202,210 202,217',
        $tempBadge
    )
    Invoke-Magick -Arguments @(
        (Join-Path $iconRoot 'device-unknown.png'),
        '-fill', '#24C8FF', '-colorize', '18',
        $tempBadge, '-compose', 'over', '-composite',
        '-depth', '8', '-strip', '-define', 'png:color-type=6',
        (Join-Path $iconRoot 'device-connecting.png')
    )
    Invoke-Magick -Arguments @(
        '-size', '256x256', 'xc:none',
        '-fill', '#35111A', '-stroke', '#FFB3BD', '-strokewidth', '5',
        '-draw', 'circle 205,205 237,205',
        '-stroke', '#FFB3BD', '-strokewidth', '9',
        '-draw', 'line 194,194 216,216',
        '-draw', 'line 216,194 194,216',
        $tempBadge
    )
    Invoke-Magick -Arguments @(
        (Join-Path $iconRoot 'device-unknown.png'),
        '-fill', '#F0445C', '-colorize', '26',
        $tempBadge, '-compose', 'over', '-composite',
        '-depth', '8', '-strip', '-define', 'png:color-type=6',
        (Join-Path $iconRoot 'device-error.png')
    )

    $deviceOffSmallSvg = Join-Path $iconRoot 'device-off-small.svg'
    $deviceOnSmallSvg = Join-Path $iconRoot 'device-on-small.svg'
    Write-Utf8NoBom -LiteralPath $deviceOffSmallSvg `
        -Value (New-DeviceSmallIconSvg 0)
    Write-Utf8NoBom -LiteralPath $deviceOnSmallSvg `
        -Value (New-DeviceSmallIconSvg 1)
    Write-IconPack `
        -SvgPath (Join-Path $assetRoot 'device-off.svg') `
        -SmallSvgPath $deviceOffSmallSvg `
        -OutputPath (Join-Path $iconRoot 'device-off.ico')
    Write-IconPack `
        -SvgPath (Join-Path $assetRoot 'device-on.svg') `
        -SmallSvgPath $deviceOnSmallSvg `
        -OutputPath (Join-Path $iconRoot 'device-on.ico')
    foreach ($state in @('unknown', 'connecting', 'error')) {
        Write-RasterIconPack `
            -SourcePath (Join-Path $iconRoot "device-$state.png") `
            -OutputPath (Join-Path $iconRoot "device-$state.ico")
    }

    New-MenuIcon 'menu-settings' '#55D3FF' @'
      <circle cx="32" cy="33" r="8"/>
      <path d="M32 16v7M32 43v7M15 33h9M40 33h9
               M20 21l6 6M38 39l6 6M44 21l-6 6M26 39l-6 6"/>
'@
    New-MenuIcon 'menu-diagnostics' '#68A8FF' @'
      <circle cx="32" cy="32" r="17"/>
      <path d="M32 29v13M32 22h.01"/>
'@
    New-MenuIcon 'menu-reconnect' '#45E2D0' @'
      <path d="M47 24a17 17 0 0 0-29-5l-4 5M17 24h-7v-8
               M17 40a17 17 0 0 0 29 5l4-5M47 40h7v8"/>
'@
    New-MenuIcon 'menu-pin' '#FFD465' @'
      <path d="M24 15h16M27 16v13l-6 8h22l-6-8V16M32 38v13"/>
'@
    New-MenuIcon 'menu-startup' '#72E3A6' @'
      <path d="M25 40l-7 6 2-10M39 40l7 6-2-10
               M32 14c8 7 11 17 7 27H25c-4-10-1-20 7-27z"/>
      <circle cx="32" cy="29" r="4"/>
'@
    New-MenuIcon 'menu-window-show' '#72E3A6' @'
      <rect x="15" y="17" width="34" height="30" rx="5"/>
      <path d="M21 25h22M32 42V28M26 34l6-6 6 6"/>
'@
    New-MenuIcon 'menu-window-hide' '#FFB86B' @'
      <rect x="15" y="17" width="34" height="30" rx="5"/>
      <path d="M21 25h22M32 29v13M26 36l6 6 6-6"/>
'@
    New-MenuIcon 'menu-exit' '#FF7890' @'
      <path d="M32 14v18M20 20a17 17 0 1 0 24 0"/>
'@

    $frames = Get-ChildItem -LiteralPath $frameRoot -Filter 'device_*.png' | Sort-Object Name | ForEach-Object FullName
    Invoke-Magick -Arguments (
        @('-delay', '3') + @($frames) +
        @('-delay', '18', $frames[-1], '-loop', '0', "APNG:$(Join-Path $assetRoot 'device-transition.apng')")
    )
}
finally {
    $resolvedAssets = [IO.Path]::GetFullPath($assetRoot).TrimEnd('\') + '\'
    $resolvedTemporary = [IO.Path]::GetFullPath($temporaryRoot)
    if ((Test-Path -LiteralPath $resolvedTemporary) -and
        $resolvedTemporary.StartsWith($resolvedAssets, [StringComparison]::OrdinalIgnoreCase)) {
        Remove-Item -LiteralPath $resolvedTemporary -Recurse -Force
    }
}

Write-PCControllerDesktopStatus -Level Success `
    -Icon (Get-PCControllerDesktopEmojiGlyph -Name Assets) -Label 'ASSETS' `
    -Message "Generated $FrameCount transition frames, vector masters, 8-bit PNGs, APNG, and exact-size multi-resolution ICO packs."
