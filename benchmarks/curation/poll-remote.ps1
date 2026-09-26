# Poll remote review jobs until terminal; fetch final job JSONs when done.
# Usage: powershell -File benchmarks/curation/poll-remote.ps1 [-Dir benchmarks/results/remote-20260926-2246] [-MaxMinutes 8]
param(
    [string]$Base = "http://121.40.235.227",
    [string]$Dir = "",
    [int]$MaxMinutes = 8
)
$ErrorActionPreference = "Stop"
$idsFile = Join-Path $Dir "job-ids.txt"
$terminal = @("completed", "completed_with_warnings", "failed", "cancelled")
$deadline = (Get-Date).AddMinutes($MaxMinutes)

function Get-JobStatus($jobId) {
    $raw = curl.exe -s -m 30 "$Base/api/reviews/$jobId"
    $raw = $raw -join "`n"
    $job = $raw | ConvertFrom-Json
    return $job
}

while ((Get-Date) -lt $deadline) {
    $pending = 0
    $lines = @()
    foreach ($line in (Get-Content $idsFile)) {
        if ($line -match '^([^=]+)=(.+)$') {
            $case = $matches[1]; $jobId = $matches[2]
            $job = Get-JobStatus $jobId
            $status = $job.status
            $lines += "$case=$jobId status=$status findings=$($job.comments.Count)"
            if ($terminal -notcontains $status) { $pending++ }
        }
    }
    Write-Output ("[{0}] pending={1}" -f (Get-Date -Format HH:mm:ss), $pending)
    $lines | ForEach-Object { Write-Output "  $_" }
    if ($pending -eq 0) {
        Write-Output "ALL TERMINAL"
        break
    }
    if ((Get-Date) -ge $deadline) { break }
    Start-Sleep -Seconds 60
}

# Fetch final snapshots for terminal jobs.
foreach ($line in (Get-Content $idsFile)) {
    if ($line -match '^([^=]+)=(.+)$') {
        $case = $matches[1]; $jobId = $matches[2]
        $out = Join-Path $Dir "$case.json"
        if (Test-Path $out) { continue }
        $job = Get-JobStatus $jobId
        if ($terminal -contains $job.status) {
            $utf8NoBom = New-Object System.Text.UTF8Encoding($false)
            [IO.File]::WriteAllText($out, ($job | ConvertTo-Json -Depth 12), $utf8NoBom)
            Write-Output "saved $case ($($job.status))"
        }
    }
}
