# Build Windows desktop exe and Linux server binary.
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$distDir = Join-Path $root "dist"
New-Item -ItemType Directory -Force -Path $distDir | Out-Null

$env:CGO_ENABLED = "0"

function Get-AppVersion {
    $path = Join-Path $root "internal\version\VERSION"
    return (Get-Content -Path $path -Raw).Trim()
}

function Get-GitCommit {
    try {
        git rev-parse --is-inside-work-tree 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) {
            return (git rev-parse --short HEAD).Trim()
        }
    } catch {}
    return ""
}

$version = Get-AppVersion
if (-not $version) { throw "internal/version/VERSION is empty" }
$commit = Get-GitCommit
$date = [DateTime]::UtcNow.ToString("yyyy-MM-dd")
$versionPkg = "github.com/hello-coder/hello-coder/internal/version"
$ldflags = "-s -w -X ${versionPkg}.Commit=$commit -X ${versionPkg}.Date=$date"
$revNote = ""
if ($commit) { $revNote = " ($commit)" }
Write-Host "building Hello Coder $version$revNote"

function Write-Utf8NoBom([string]$Path, [string]$Content) {
    $utf8 = New-Object System.Text.UTF8Encoding $false
    [System.IO.File]::WriteAllText($Path, $Content, $utf8)
}

# --- Windows (desktop window) ---
$winName = "hello-coder-$version-windows-amd64"
$winDir = Join-Path $distDir $winName
if (Test-Path $winDir) { Remove-Item -Recurse -Force $winDir }
New-Item -ItemType Directory -Force -Path $winDir | Out-Null

$env:GOOS = "windows"
$env:GOARCH = "amd64"
$ico = Join-Path $root "internal\desktop\app.ico"
$syso = Join-Path $root "cmd\server\rsrc_windows_amd64.syso"
if (Test-Path $ico) {
    go run github.com/akavel/rsrc@v0.10.2 -arch amd64 -ico $ico -o $syso
}
go build -trimpath -ldflags $ldflags -o "$winDir\hello-coder.exe" ./cmd/server

$winReadme = @"
Hello Coder $version — Windows 本机版
============================

双击 hello-coder.exe 即可启动，会打开独立窗口（无需登录）。
关闭窗口即退出程序。
查看版本：hello-coder.exe --version

数据目录
  与 exe 同级的 data\ 会在首次启动时自动创建（SQLite、密钥、窗口配置）。
  备份/迁移时请一并拷贝 data 目录。
  日志：data\hello-coder.log

若要以服务方式给局域网浏览器访问（不弹独立窗口，需要登录）：
  双击 start-server.bat
  或设置 HELLO_CODER_MODE=server 后运行 exe
  然后访问 http://本机IP:10240/

可选环境变量 / 参数
  --version                 打印版本后退出
  --mode desktop|server     覆盖默认模式（Windows 默认为 desktop）
  HELLO_CODER_MODE          同上
  HELLO_CODER_AUTH          on|off 强制开/关登录（默认跟随 mode）
  HELLO_CODER_ADDR          监听地址（desktop 默认 127.0.0.1:10240）
  HELLO_CODER_DATA_DIR      数据目录，默认 .\data
  HELLO_CODER_JWT_SECRET    JWT 密钥（生产务必修改）
  HELLO_CODER_JWT_TTL_HOURS JWT 有效期小时数，默认 4
  HELLO_CODER_DATA_KEY      连接密码落库加密密钥（生产务必单独设置）
  HELLO_CODER_LOG_LEVEL     日志级别，默认 info

需要 Windows 10+ 且已安装 Edge / WebView2 运行时（系统通常已自带）。
"@
Write-Utf8NoBom -Path "$winDir\README.txt" -Content $winReadme

$serverBat = @"
@echo off
cd /d "%~dp0"
set HELLO_CODER_MODE=server
if not exist "data" mkdir "data"
echo Hello Coder server mode
echo Open http://localhost:10240/ in a browser
echo Close this window to stop the server.
echo.
hello-coder.exe --mode server
if errorlevel 1 pause
"@
Set-Content -Path "$winDir\start-server.bat" -Value $serverBat -Encoding ASCII

$winZip = Join-Path $distDir "$winName.zip"
if (Test-Path $winZip) { Remove-Item -Force $winZip }
Compress-Archive -Path $winDir -DestinationPath $winZip

# --- Linux (browser access) ---
$linuxName = "hello-coder-$version-linux-amd64"
$linuxDir = Join-Path $distDir $linuxName
if (Test-Path $linuxDir) { Remove-Item -Recurse -Force $linuxDir }
New-Item -ItemType Directory -Force -Path $linuxDir | Out-Null

$env:GOOS = "linux"
$env:GOARCH = "amd64"
go build -trimpath -ldflags $ldflags -o "$linuxDir\hello-coder" ./cmd/server

Copy-Item (Join-Path $PSScriptRoot "hello-coder.service") "$linuxDir\hello-coder.service"

$linuxReadme = @"
Hello Coder $version — Linux 服务版
==========================

chmod +x hello-coder
./hello-coder --version
./hello-coder

然后用浏览器访问（需要注册/登录）：
  http://<服务器IP>:10240/

生产建议设置：
  export HELLO_CODER_MODE=server
  export HELLO_CODER_ADDR=:10240
  export HELLO_CODER_DATA_DIR=/var/lib/hello-coder
  export HELLO_CODER_JWT_SECRET='换成随机长串'
  export HELLO_CODER_DATA_KEY='换成另一串'

systemd 示例见同目录 hello-coder.service。
"@
Write-Utf8NoBom -Path "$linuxDir\README.txt" -Content $linuxReadme

$linuxTar = Join-Path $distDir "$linuxName.tar.gz"
if (Test-Path $linuxTar) { Remove-Item -Force $linuxTar }
tar -czf $linuxTar -C $distDir $linuxName

$winExe = Get-Item "$winDir\hello-coder.exe"
$linuxBin = Get-Item "$linuxDir\hello-coder"
Write-Host "version: $version"
Write-Host "windows: $($winExe.FullName) ($([math]::Round($winExe.Length/1MB, 2)) MB)"
Write-Host "linux:   $($linuxBin.FullName) ($([math]::Round($linuxBin.Length/1MB, 2)) MB)"
Write-Host "package: $winZip"
Write-Host "package: $linuxTar"
