# release.ps1 — AILauncher 发布：构建最新二进制并同步到生产目录。
#
# 触发时机（满足任一即执行本脚本）：
#   · 小版本迭代（改动落地后）
#   · 功能增加
#   · 版本发布(Release)
#   · Git 支链创建/切换
#
# 部署目录不在仓库里，由参数或环境变量给出：
#   $env:AILAUNCHER_DEPLOY_DIR = '<你的部署目录>'    # 设一次，之后可无参运行
#
# 用法：
#   .\release.ps1                 # 全量验证 + 构建 + 同步到 AILAUNCHER_DEPLOY_DIR
#   .\release.ps1 -Target D:\x    # 指定目标目录
#   .\release.ps1 -SkipTests      # 跳过测试（仅紧急同步时用）
#   .\release.ps1 -WhatIf         # 只验证+构建，不同步
#
# 同步内容：ailauncher.exe + config.json（覆盖前自动按时间戳备份）。

param(
    [string]$Target = '',
    [switch]$SkipTests,
    [switch]$WhatIf
)

$ErrorActionPreference = 'Stop'
Set-Location $PSScriptRoot

function Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Note($msg) { Write-Host "    $msg" -ForegroundColor DarkGray }
function Ok($msg) { Write-Host "    $msg" -ForegroundColor Green }
function Fail($msg) { Write-Host "!! $msg" -ForegroundColor Red; exit 1 }

# 部署目录：-Target 优先，其次 AILAUNCHER_DEPLOY_DIR。先解析以快速失败；
# -WhatIf 只验证+构建、不同步，故不要求目标目录。
if (-not $WhatIf -and -not $Target) { $Target = $env:AILAUNCHER_DEPLOY_DIR }
if (-not $WhatIf -and -not $Target) {
    Write-Host '!! 未指定部署目录。' -ForegroundColor Red
    Write-Host "   设环境变量：`$env:AILAUNCHER_DEPLOY_DIR = '<你的部署目录>'" -ForegroundColor DarkGray
    Write-Host '   或指定参数：.\release.ps1 -Target <目录>' -ForegroundColor DarkGray
    exit 2
}

# 1. 验证：gofmt / vet / test
Step 'gofmt 检查'
$unformatted = gofmt -l ./core ./cli ./ui
if ($unformatted) { Fail "gofmt 未过：`n$unformatted" }
Ok 'gofmt 干净'

Step 'go vet'
go vet ./core/... ./cli/... ./ui/...
if (-not $?) { Fail 'go vet 未过' }
Ok 'vet 通过'

if ($SkipTests) {
    Write-Host '    已跳过测试' -ForegroundColor Yellow
} else {
    Step 'go test（core / cli / ui）'
    go test ./core/... ./cli/... ./ui/... -count=1
    if (-not $?) { Fail 'go test 未过' }
    Ok '三模块测试全绿'
}

# 2. 构建
Step '构建 ailauncher.exe'
go build -o ailauncher.exe ./ui/cmd/ailauncher
if (-not $?) { Fail '构建失败' }
$built = Get-Item (Join-Path $PSScriptRoot 'ailauncher.exe')
Ok ("{0:N0} 字节  {1}" -f $built.Length, $built.LastWriteTime)

if ($WhatIf) {
    if ($Target) {
        Write-Host "`n已跳过同步（-WhatIf）。目标目录为 $Target" -ForegroundColor Yellow
    } else {
        Write-Host "`n已跳过同步（-WhatIf）。未指定目标目录（-WhatIf 不要求）。" -ForegroundColor Yellow
    }
    exit 0
}

# 3. 目标目录
Step "目标目录 $Target"
if (-not (Test-Path $Target)) {
    Note '不存在，创建'
    New-Item -ItemType Directory -Force -Path $Target | Out-Null
}

# 4. 占用检测：生产 exe 在跑会导致复制失败
$targetExe = Join-Path $Target 'ailauncher.exe'
if (Test-Path $targetExe) {
    $running = Get-Process -Name 'ailauncher' -ErrorAction SilentlyContinue
    if ($running) { Fail "生产 ailauncher 正在运行（PID $($running.Id -join ',')），先关闭再发布" }
}

# 5. 备份 + 同步
Step "同步到 $Target"
$ts = Get-Date -Format 'yyyyMMdd-HHmmss'
foreach ($f in @('ailauncher.exe', 'config.json')) {
    $dst = Join-Path $Target $f
    if (Test-Path $dst) {
        Copy-Item $dst "$dst.bak-$ts" -Force
        Note "备份 $f → $f.bak-$ts"
    }
    Copy-Item (Join-Path $PSScriptRoot $f) $dst -Force
    Ok "同步 $f"
}

# 6. 烟雾测试
Step '烟雾测试（生产二进制 version）'
& $targetExe version
if (-not $?) { Fail '生产二进制执行失败' }

Write-Host "`n发布完成：$Target" -ForegroundColor Green
