# ============================================================================
# quickidc 一键安装（Windows）
#
#   稳定版：
#     irm https://idcgit.opkg.cn/quickidc-install.ps1 | iex
#   测试版：
#     & ([scriptblock]::Create((irm .../quickidc-install.ps1))) -Channel beta
#   指定版本 / 目录 / 离线包：
#     .\quickidc-install.ps1 -Version 1.2.3 -Dir "C:\Tools\quickidc"
#     .\quickidc-install.ps1 -File .\quickidc-windows-amd64.exe
# ============================================================================
param(
    [ValidateSet("stable", "beta")]
    [string]$Channel = "stable",
    [string]$Version = "",
    [string]$Dir = "$env:LOCALAPPDATA\quickidc\bin",
    [string]$File = ""
)

$ErrorActionPreference = "Stop"
$repo = if ($env:QUICKIDC_REPO) { $env:QUICKIDC_REPO } else { "OpenIDCSTeam/CloudHost" }
$downloadBase = $env:QUICKIDC_DOWNLOAD_BASE
$arch = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture) {
    "X64" { "amd64" }
    "Arm64" { "arm64" }
    default { throw "不支持的架构: $_" }
}
$asset = "quickidc-windows-$arch.exe"

if (-not $File) {
    $tag = ""
    if ($Version) {
        $tag = "v$($Version.TrimStart('v'))"
    } elseif ($Channel -eq "beta") {
        $tag = "beta"
    } else {
        Write-Host "==> 查询最新稳定版"
        $release = Invoke-RestMethod -Uri "https://api.github.com/repos/$repo/releases/latest" `
            -Headers @{ "User-Agent" = "quickidc-installer" }
        $tag = $release.tag_name
    }
    if (-not $tag) { throw "无法确定版本，请用 -Version 指定" }

    if ($downloadBase) {
        $url = "$($downloadBase.TrimEnd('/'))/$tag/$asset"
    } else {
        $url = "https://github.com/$repo/releases/download/$tag/$asset"
    }
    $tmp = Join-Path $env:TEMP "quickidc-download.exe"
    Write-Host "==> 通道 $Channel，下载 $url"
    Invoke-WebRequest -Uri $url -OutFile $tmp -UseBasicParsing
} else {
    if (-not (Test-Path $File)) { throw "文件不存在: $File" }
    $tmp = $File
}

New-Item -ItemType Directory -Force -Path $Dir | Out-Null
$target = Join-Path $Dir "quickidc.exe"
Copy-Item -Path $tmp -Destination $target -Force
if ($tmp -ne $File) { Remove-Item $tmp -Force }

Write-Host "==> 已安装到 $target"
& $target version

$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$Dir*") {
    [Environment]::SetEnvironmentVariable("Path", "$userPath;$Dir", "User")
    Write-Host "==> 已将 $Dir 加入用户 PATH（新终端生效）"
}
