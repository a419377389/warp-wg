param(
  [string]$Target = "host",
  [string]$Arch = "amd64",
  [switch]$IncludeGateway = $true
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$gatewayRoot = Resolve-Path (Join-Path $scriptDir "..")
$repoRoot = Resolve-Path (Join-Path $gatewayRoot "..")
$dist = Join-Path $gatewayRoot "dist"

New-Item -ItemType Directory -Force -Path $dist | Out-Null

function Get-HostTarget {
  if ($env:OS -eq "Windows_NT") {
    return @{ os = "windows"; arch = $Arch }
  }
  $uname = ""
  try { $uname = (uname) } catch { $uname = "" }
  if ($uname -match "Darwin") {
    return @{ os = "darwin"; arch = $Arch }
  }
  return @{ os = "linux"; arch = $Arch }
}

$targets = @()
switch ($Target.ToLower()) {
  "host" { $targets = @($(Get-HostTarget)) }
  "all" {
    $targets = @(
      @{ os = "windows"; arch = $Arch },
      @{ os = "darwin"; arch = $Arch },
      @{ os = "linux"; arch = $Arch }
    )
  }
  default { $targets = @(@{ os = $Target.ToLower(); arch = $Arch }) }
}

foreach ($t in $targets) {
  $targetDir = Join-Path $dist "$($t.os)-$($t.arch)"
  New-Item -ItemType Directory -Force -Path $targetDir | Out-Null

  $bin = "warp-gateway"
  if ($t.os -eq "windows") {
    $bin += ".exe"
  }

  $env:GOOS = $t.os
  $env:GOARCH = $t.arch
  $env:CGO_ENABLED = "1"

  $ldflags = "-s -w"
  if ($t.os -eq "windows") {
    $ldflags = "-s -w -H=windowsgui"
  }

  Push-Location $gatewayRoot
  go build -trimpath -ldflags $ldflags -o (Join-Path $targetDir $bin) .
  Pop-Location

  $webDst = Join-Path $targetDir "web"
  if (Test-Path $webDst) { Remove-Item -Recurse -Force $webDst }
  Copy-Item -Recurse -Force (Join-Path $gatewayRoot "web") $webDst

  $assetsDst = Join-Path $targetDir "assets"
  if (Test-Path $assetsDst) { Remove-Item -Recurse -Force $assetsDst }
  Copy-Item -Recurse -Force (Join-Path $gatewayRoot "assets") $assetsDst

  if ($IncludeGateway) {
    $backendDst = Join-Path $targetDir "backend"
    if (Test-Path $backendDst) { Remove-Item -Recurse -Force $backendDst }
    Copy-Item -Recurse -Force (Join-Path $repoRoot "backend") $backendDst

    $resourcesDst = Join-Path $targetDir "resources"
    if (Test-Path $resourcesDst) { Remove-Item -Recurse -Force $resourcesDst }
    Copy-Item -Recurse -Force (Join-Path $repoRoot "resources") $resourcesDst
  }

  New-Item -ItemType Directory -Force -Path (Join-Path $targetDir "data") | Out-Null
  New-Item -ItemType Directory -Force -Path (Join-Path $targetDir "logs") | Out-Null
}

Write-Host "Build complete:" $dist
