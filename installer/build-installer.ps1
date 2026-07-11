[CmdletBinding()]
param(
    [ValidatePattern('^\d+\.\d+\.\d+$')]
    [string]$Version = '0.0.1',

    [string]$InnoSetupPath
)

Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$repositoryRoot = [IO.Path]::GetFullPath((Split-Path $PSScriptRoot -Parent))
$projectPath = Join-Path $repositoryRoot 'StageManager\StageManager.csproj'
$artifactsRoot = [IO.Path]::GetFullPath((Join-Path $repositoryRoot 'artifacts'))
$publishDirectory = Join-Path $artifactsRoot 'publish\win-x64'
$installerDirectory = Join-Path $artifactsRoot 'installer'
$versionInfoVersion = "$Version.0"

function Remove-BuildDirectory {
    param([Parameter(Mandatory)][string]$Path)

    $resolvedPath = [IO.Path]::GetFullPath($Path)
    $allowedPrefix = $artifactsRoot.TrimEnd([IO.Path]::DirectorySeparatorChar) + [IO.Path]::DirectorySeparatorChar
    if (-not $resolvedPath.StartsWith($allowedPrefix, [StringComparison]::OrdinalIgnoreCase)) {
        throw "拒绝清理 artifacts 目录之外的路径：$resolvedPath"
    }

    if (Test-Path -LiteralPath $resolvedPath) {
        Remove-Item -LiteralPath $resolvedPath -Recurse -Force
    }
}

$dotnet = Get-Command dotnet -ErrorAction Stop
$installedSdks = & $dotnet.Source --list-sdks
if (-not $installedSdks) {
    throw '未找到 .NET SDK。请安装 .NET 8 SDK 后重试。'
}

Remove-BuildDirectory -Path $publishDirectory
New-Item -ItemType Directory -Path $publishDirectory -Force | Out-Null
New-Item -ItemType Directory -Path $installerDirectory -Force | Out-Null

& $dotnet.Source publish $projectPath `
    --configuration Release `
    --runtime win-x64 `
    --self-contained true `
    --output $publishDirectory `
    -p:PublishSingleFile=true `
    -p:IncludeNativeLibrariesForSelfExtract=true `
    -p:EnableCompressionInSingleFile=true `
    -p:DebugSymbols=false `
    -p:DebugType=None `
    -p:Version=$Version `
    -p:AssemblyVersion=$versionInfoVersion `
    -p:FileVersion=$versionInfoVersion
if ($LASTEXITCODE -ne 0) {
    throw "dotnet publish 失败，退出代码：$LASTEXITCODE"
}

$compilerCandidates = @(
    $InnoSetupPath,
    (Join-Path $env:LOCALAPPDATA 'Programs\Inno Setup 6\ISCC.exe'),
    (Join-Path ${env:ProgramFiles(x86)} 'Inno Setup 6\ISCC.exe'),
    (Join-Path $env:ProgramFiles 'Inno Setup 6\ISCC.exe')
) | Where-Object { -not [string]::IsNullOrWhiteSpace($_) }

$compilerPath = $compilerCandidates |
    Where-Object { Test-Path -LiteralPath $_ -PathType Leaf } |
    Select-Object -First 1
if (-not $compilerPath) {
    $isccCommand = Get-Command ISCC.exe -ErrorAction SilentlyContinue
    if ($null -ne $isccCommand) {
        $compilerPath = $isccCommand.Source
    }
}

if (-not $compilerPath) {
    throw '未找到 Inno Setup 6。请安装后重试，或通过 -InnoSetupPath 指定 ISCC.exe。'
}

$innoScript = Join-Path $PSScriptRoot 'StageManager.iss'
& $compilerPath `
    "/DMyAppVersion=$Version" `
    "/DMyVersionInfoVersion=$versionInfoVersion" `
    "/DSourceDir=$publishDirectory" `
    "/DOutputDir=$installerDirectory" `
    $innoScript
if ($LASTEXITCODE -ne 0) {
    throw "Inno Setup 编译失败，退出代码：$LASTEXITCODE"
}

$installerPath = Join-Path $installerDirectory "StageBar-Setup-$Version-x64.exe"
if (-not (Test-Path -LiteralPath $installerPath -PathType Leaf)) {
    throw "安装程序未生成：$installerPath"
}

Write-Output "安装程序已生成：$installerPath"
