<#
 @Author        : 顾青离
 @Url           : sucaijun.com
 @Email         : Ricky@LiHai.La
 @Project       : CodexRelay
 @Description   : Codex API 中转热切换桌面工具
 @File          : Windows 原生应用构建入口
#>
$ErrorActionPreference = "Stop"

$projectRoot = $PSScriptRoot
$outputDirectory = Join-Path $projectRoot "dist"
$buildDirectory = Join-Path $projectRoot "build\windows"
$commandDirectory = Join-Path $projectRoot "cmd"
$frontendDirectory = Join-Path $projectRoot "frontend"
$bindingsDirectory = Join-Path $frontendDirectory "bindings"
$iconCacheDirectory = Join-Path $projectRoot ".tmp\wails-icons"
$logo = Join-Path $projectRoot "logo.png"
$versionSource = Join-Path $projectRoot "internal\desktop\service.go"
$versionPattern = '(?:const|var)\s+applicationVersion\s*=\s*"([^"]+)"'
$versionMatches = @(Select-String -LiteralPath $versionSource -Pattern $versionPattern)
if ($versionMatches.Count -ne 1) {
    throw "无法从 $versionSource 唯一读取 applicationVersion"
}
$applicationVersion = $versionMatches[0].Matches[0].Groups[1].Value
if ($applicationVersion -notmatch '^\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?$') {
    throw "applicationVersion 不是有效版本号：$applicationVersion"
}
$versionedExecutable = Join-Path $outputDirectory "CodexRelay-$applicationVersion.exe"

New-Item -ItemType Directory -Force -Path $outputDirectory,$buildDirectory,$iconCacheDirectory | Out-Null
# dist 只保留当前构建产物；该目录是脚本管理的发布输出目录。
Get-ChildItem -LiteralPath $outputDirectory -Filter "CodexRelay*.exe" -File -ErrorAction SilentlyContinue | Remove-Item -Force

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$env:CGO_ENABLED = "0"
$env:GOTELEMETRY = "off"
$env:GOPROXY = "https://goproxy.cn,direct"

Push-Location $projectRoot
try {
    $wails = "github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-beta.11"
    go run $wails generate icons -input $logo -windowsfilename (Join-Path $buildDirectory "app.ico") -macfilename (Join-Path $iconCacheDirectory "icon.icns")
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go run $wails generate syso -arch amd64 -icon (Join-Path $buildDirectory "app.ico") -manifest (Join-Path $buildDirectory "app.manifest") -out (Join-Path $commandDirectory "resource_windows_amd64.syso")
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go run $wails generate bindings -b -d $bindingsDirectory -clean ./internal/desktop
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go test ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    go vet ./...
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    $frontendScripts = Get-ChildItem -LiteralPath $frontendDirectory -Recurse -Filter "*.js" -File | Where-Object {
        $relativePath = [System.IO.Path]::GetRelativePath($frontendDirectory, $_.FullName)
        $relativePath -notlike "bindings\*" -and $relativePath -notlike "vendor\*"
    }
    foreach ($script in $frontendScripts) {
        node --check $script.FullName
        if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
    }
    go build -trimpath -tags production -ldflags "-H windowsgui -s -w" -o $versionedExecutable ./cmd
    if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
} finally {
    Pop-Location
}

Write-Host "Built: $versionedExecutable"
