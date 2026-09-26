# Create real-PR review jobs on the deployed service and save the responses.
# Usage: powershell -File benchmarks/curation/run-remote.ps1 [-Base http://121.40.235.227] [-Out benchmarks/results/remote-20260926-r2]
param(
    [string]$Base = "http://121.40.235.227",
    [string]$Out = ""
)
$ErrorActionPreference = "Stop"
if ($Out -eq "") { $Out = "benchmarks/results/remote-" + (Get-Date -Format "yyyyMMdd-HHmm") }
New-Item -ItemType Directory -Force -Path $Out | Out-Null

$manifest = [IO.File]::ReadAllText("$PWD/benchmarks/real_pr_v1.json", [Text.Encoding]::UTF8) | ConvertFrom-Json
$ids = @()
foreach ($case in $manifest) {
    $bodyFile = Join-Path $env:TEMP "cr-agent-create-$($case.id).json"
    # 无 BOM：gin 的 JSON 绑定会 BOM 拒绑（与 CRLF 路径污染同族问题）。
    $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
    [IO.File]::WriteAllText($bodyFile, ('{"source":"' + $case.pr_url + '","budget_yuan":10}'), $utf8NoBom)
    $response = curl.exe -s -m 30 -X POST -H "Content-Type: application/json" --data-binary "@$bodyFile" "$Base/api/reviews" | ConvertFrom-Json
    Remove-Item $bodyFile -ErrorAction SilentlyContinue
    if (-not $response.id) {
        Write-Output "[$($case.id)] CREATE FAILED: $response"
        continue
    }
    $ids += "$($case.id)=$($response.id)"
    [IO.File]::WriteAllText((Join-Path $Out "$($case.id)-create.json"), ($response | ConvertTo-Json -Depth 12), [Text.Encoding]::UTF8)
    Write-Output "[$($case.id)] job=$($response.id) status=$($response.status)"
}
[IO.File]::WriteAllText((Join-Path $Out "job-ids.txt"), ($ids -join "`n") + "`n", [Text.Encoding]::UTF8)
Write-Output "OUT=$Out"
