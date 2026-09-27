# Rerun only the round-2 core-recall badcases (3 valid-reference cases) on the
# deployed service, poll to terminal state, and save final job JSONs.
# Usage: powershell -File benchmarks/curation/rerun-badcase.ps1 [-Base http://121.40.235.227] [-Out benchmarks/results/remote-20260927-badcase]
param(
    [string]$Base = "http://121.40.235.227",
    [string]$Out = ""
)
$ErrorActionPreference = "Stop"
if ($Out -eq "") { $Out = "benchmarks/results/remote-" + (Get-Date -Format "yyyyMMdd-HHmm") }
New-Item -ItemType Directory -Force -Path $Out | Out-Null

# The three audited valid-reference cases (core recall 0/3 in round 2).
$badcases = @(
    "https://github.com/nodejs/node/pull/41749",
    "https://github.com/gin-gonic/gin/pull/2632",
    "https://github.com/pandas-dev/pandas/pull/22862"
)

$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
$ids = @()
foreach ($prUrl in $badcases) {
    $caseId = ($prUrl -replace '^.*/', '' -replace '/pull/', '-')
    $name = "case-$caseId"
    $bodyFile = Join-Path $env:TEMP "cr-agent-create-$caseId.json"
    # No BOM: gin's JSON binding rejects BOM-prefixed bodies.
    [IO.File]::WriteAllText($bodyFile, ('{"source":"' + $prUrl + '","budget_yuan":10}'), $utf8NoBom)
    $response = curl.exe -s -m 30 -X POST -H "Content-Type: application/json" --data-binary "@$bodyFile" "$Base/api/reviews" | ConvertFrom-Json
    Remove-Item $bodyFile -ErrorAction SilentlyContinue
    if (-not $response.id) {
        Write-Output "[$name] CREATE FAILED: $response"
        continue
    }
    $ids += "$name=$($response.id)"
    [IO.File]::WriteAllText((Join-Path $Out "$name-create.json"), ($response | ConvertTo-Json -Depth 12), $utf8NoBom)
    Write-Output "[$name] job=$($response.id) status=$($response.status)"
}
[IO.File]::WriteAllText((Join-Path $Out "job-ids.txt"), ($ids -join "`n") + "`n", [Text.Encoding]::UTF8)
if ($ids.Count -eq 0) { Write-Output "NO JOBS CREATED"; exit 1 }

# Poll until terminal (mirror of poll-remote.ps1, self-contained).
$terminal = @("completed", "completed_with_warnings", "failed", "cancelled")
$deadline = (Get-Date).AddMinutes(30)
while ((Get-Date) -lt $deadline) {
    $pending = 0
    $lines = @()
    foreach ($line in (Get-Content (Join-Path $Out "job-ids.txt"))) {
        if ($line -match '^([^=]+)=(.+)$') {
            $name = $matches[1]; $jobId = $matches[2]
            $job = curl.exe -s -m 30 "$Base/api/reviews/$jobId" | ConvertFrom-Json
            $status = $job.status
            $lines += "$name status=$status findings=$($job.comments.Count)"
            if ($terminal -notcontains $status) { $pending++ }
            elseif (-not (Test-Path (Join-Path $Out "$name.json"))) {
                [IO.File]::WriteAllText((Join-Path $Out "$name.json"), ($job | ConvertTo-Json -Depth 12), $utf8NoBom)
            }
        }
    }
    Write-Output ("[{0}] pending={1}" -f (Get-Date -Format HH:mm:ss), $pending)
    $lines | ForEach-Object { Write-Output "  $_" }
    if ($pending -eq 0) { Write-Output "ALL TERMINAL"; break }
    Start-Sleep -Seconds 60
}
Write-Output "OUT=$Out"
