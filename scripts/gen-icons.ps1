param(
  [string]$Source = ""
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$gatewayRoot = Resolve-Path (Join-Path $scriptDir "..")
$repoRoot = Resolve-Path (Join-Path $gatewayRoot "..")
$assets = Join-Path $gatewayRoot "assets"

New-Item -ItemType Directory -Force -Path $assets | Out-Null

if ([string]::IsNullOrWhiteSpace($Source)) {
  $Source = Join-Path $repoRoot "icon.ico"
}

if (-not (Test-Path $Source)) {
  throw "icon source not found: $Source"
}

Copy-Item -Force $Source (Join-Path $assets "icon.ico")

Add-Type -AssemblyName System.Drawing
$icon = New-Object System.Drawing.Icon($Source)
$bmp = $icon.ToBitmap()
$pngPath = Join-Path $assets "icon.png"
$bmp.Save($pngPath, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Dispose()
$icon.Dispose()

Write-Host "Icons ready:" $assets
