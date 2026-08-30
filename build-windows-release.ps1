param(
    [ValidateSet("amd64", "arm64")]
    [string]$Architecture = "amd64",
    [Parameter(Mandatory = $true)]
    [string]$Version,
    [switch]$SkipChecks
)

$ErrorActionPreference = "Stop"

$projectRoot = $PSScriptRoot
$outputDirectory = Join-Path $projectRoot "dist"
$buildDirectory = Join-Path $projectRoot "build\windows"
$commandDirectory = Join-Path $projectRoot "cmd"
$frontendDirectory = Join-Path $projectRoot "frontend"
$bindingsDirectory = Join-Path $frontendDirectory "bindings"
$iconCacheDirectory = Join-Path $projectRoot ".tmp\wails-icons"
$webviewDirectory = Join-Path $iconCacheDirectory "webview2"
$logo = Join-Path $projectRoot "logo.png"
$installerScript = Join-Path $buildDirectory "CodexRelay.nsi"
$installerIcon = Join-Path $buildDirectory "app.ico"
$webviewBootstrapper = Join-Path $webviewDirectory "MicrosoftEdgeWebview2Setup.exe"

$normalizedVersion = $Version.Trim()
if ($normalizedVersion.StartsWith("v")) {
    $normalizedVersion = $normalizedVersion.Substring(1)
}
if ($normalizedVersion -notmatch '^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') {
    throw "版本号不是有效的 SemVer：$Version"
}
$fileVersion = (($normalizedVersion -split '[+-]', 2)[0]) + ".0"

$versionedExecutable = Join-Path $outputDirectory "CodexRelay-$normalizedVersion-$Architecture.exe"
$displayArchitecture = if ($Architecture -eq "amd64") { "x64" } else { $Architecture }
$installer = Join-Path $outputDirectory "CodexRelay-$normalizedVersion-windows-$displayArchitecture-setup.exe"
$syso = Join-Path $commandDirectory "resource_windows_$Architecture.syso"

New-Item -ItemType Directory -Force -Path $outputDirectory, $buildDirectory, $iconCacheDirectory, $webviewDirectory | Out-Null

# Wails CLI 必须使用 runner 自身的架构运行；目标架构只在最终 Go 编译时切换。
$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
$env:GOTELEMETRY = "off"

Push-Location $projectRoot
try {
    $wails = "github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.11"
    go run $wails generate webview2bootstrapper -dir $webviewDirectory
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go run $wails generate icons -input $logo -windowsfilename (Join-Path $buildDirectory "app.ico") -macfilename (Join-Path $iconCacheDirectory "icon.icns")
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go run $wails generate syso -arch $Architecture -icon (Join-Path $buildDirectory "app.ico") -manifest (Join-Path $buildDirectory "app.manifest") -out $syso
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go run $wails generate bindings -b -d $bindingsDirectory -clean ./internal/desktop
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    if (-not $SkipChecks) {
        go test ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
        go vet ./...
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    $frontendScripts = Get-ChildItem -LiteralPath $frontendDirectory -Recurse -Filter "*.js" -File | Where-Object {
        $relativePath = [System.IO.Path]::GetRelativePath($frontendDirectory, $_.FullName)
        $relativePath -notlike "bindings\*" -and $relativePath -notlike "vendor\*"
    }
    foreach ($script in $frontendScripts) {
        node --check $script.FullName
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }

    $env:GOARCH = $Architecture
    go build -trimpath -tags production -ldflags "-H windowsgui -s -w -X codexrelay/internal/desktop.applicationVersion=$normalizedVersion" -o $versionedExecutable ./cmd
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

    & makensis "/DAPP_VERSION=$normalizedVersion" "/DAPP_FILE_VERSION=$fileVersion" "/DAPP_ARCH=$Architecture" "/DAPP_EXE=$versionedExecutable" "/DAPP_OUTPUT=$installer" "/DAPP_ICON=$installerIcon" "/DAPP_WEBVIEW2=$webviewBootstrapper" $installerScript
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}

Write-Host "Built: $versionedExecutable"
Write-Host "Packaged: $installer"
