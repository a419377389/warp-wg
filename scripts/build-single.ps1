param(
  [string]$OS = "host",
  [string]$Arch = "amd64,arm64",
  [string]$OutDir = "",
  [string]$Tags = ""
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$gatewayRoot = Resolve-Path (Join-Path $scriptDir "..")
$distRoot = if ($OutDir -ne "") { $OutDir } else { Join-Path $gatewayRoot "dist" }
$singleDir = Join-Path $distRoot "single"

New-Item -ItemType Directory -Force -Path $singleDir | Out-Null

function Get-HostOS {
  if ($env:OS -eq "Windows_NT") {
    return "windows"
  }
  $uname = ""
  try { $uname = (uname) } catch { $uname = "" }
  if ($uname -match "Darwin") {
    return "darwin"
  }
  return "linux"
}

function Split-List([string]$value) {
  $items = $value.Split(",") | ForEach-Object { $_.Trim().ToLower() } | Where-Object { $_ -ne "" }
  return ,$items
}

$hostOS = Get-HostOS
$hostArch = (go env GOARCH).Trim().ToLower()
$osInput = $OS.Trim().ToLower()
$archInput = $Arch.Trim().ToLower()

$osList = @()
switch ($osInput) {
  "host" { $osList = @($hostOS) }
  "all" { $osList = @("windows", "darwin", "linux") }
  default { $osList = Split-List $osInput }
}

$archList = @()
if ($archInput -eq "all") {
  $archList = @("amd64", "arm64")
} else {
  $archList = Split-List $archInput
}

foreach ($osName in $osList) {
  foreach ($archName in $archList) {
    if ($osName -eq "" -or $archName -eq "") {
      continue
    }

    $needsCgo = $osName -ne "windows"
    $isCrossOS = $osName -ne $hostOS
    if ($isCrossOS -and $needsCgo) {
      Write-Host "Skip ${osName}/${archName}: cgo cross-compile not supported on $hostOS"
      continue
    }

    $isCrossArch = $archName -ne $hostArch
    $ccEnvKey = ("CC_{0}_{1}" -f $osName, $archName).ToUpper().Replace("-", "_")
    $cxxEnvKey = ("CXX_{0}_{1}" -f $osName, $archName).ToUpper().Replace("-", "_")
    $ccValue = [Environment]::GetEnvironmentVariable($ccEnvKey)
    $cxxValue = [Environment]::GetEnvironmentVariable($cxxEnvKey)

    if ($osName -eq "linux" -and $hostOS -eq "linux" -and $isCrossArch -and $needsCgo -and [string]::IsNullOrWhiteSpace($ccValue) -and [string]::IsNullOrWhiteSpace($env:CC)) {
      Write-Host "Skip ${osName}/${archName}: set $ccEnvKey for cgo cross-compile"
      continue
    }

    $binName = "warp-gateway"
    if ($osName -eq "windows") {
      $binName += ".exe"
    }
    $outName = "warp-gateway_$osName" + "_" + "$archName"
    if ($osName -eq "windows") {
      $outName += ".exe"
    }
    $outPath = Join-Path $singleDir $outName

    $env:GOOS = $osName
    $env:GOARCH = $archName
    $env:CGO_ENABLED = if ($needsCgo) { "1" } else { "0" }
    if (-not [string]::IsNullOrWhiteSpace($ccValue)) {
      $env:CC = $ccValue
    }
    if (-not [string]::IsNullOrWhiteSpace($cxxValue)) {
      $env:CXX = $cxxValue
    }

    $tagArgs = @()
    if (-not [string]::IsNullOrWhiteSpace($Tags)) {
      $tagArgs = @("-tags", $Tags)
    }

    $ldflags = "-s -w"
    if ($osName -eq "windows") {
      $ldflags = "-s -w -H=windowsgui"
    }

    Push-Location $gatewayRoot
    go build -trimpath -ldflags $ldflags @tagArgs -o $outPath .
    Pop-Location

    Write-Host "Built $outPath"
  }
}

Write-Host "Build complete: $singleDir"
