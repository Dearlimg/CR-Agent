# Fetch trace details (tool input/output) for saved remote job results.
# Usage: powershell -File benchmarks/curation/fetch-trace-details.ps1 [-Base http://121.40.235.227] [-Dir benchmarks/results/remote-20260926]
# Job JSONs carry skeleton traces only (input/output live in the store, served
# by GET /api/reviews/:id/traces/:traceID). This script joins them per job and
# writes trace-details/<jobID>.json next to the job files.
param(
    [string]$Base = "http://121.40.235.227",
    [string]$Dir = "benchmarks/results/remote-20260926"
)
$ErrorActionPreference = "Stop"
$outDir = Join-Path $Dir "trace-details"
New-Item -ItemType Directory -Force -Path $outDir | Out-Null

$jobFiles = Get-ChildItem $Dir -Filter "*.json" | Where-Object { $_.Name -notmatch 'trace-details' -and $_.Name -match '^[a-z]+-\d+\.json$' }
foreach ($file in $jobFiles) {
    $job = [IO.File]::ReadAllText($file.FullName, [Text.Encoding]::UTF8) | ConvertFrom-Json
    $outPath = Join-Path $outDir $job.id
    if (Test-Path $outPath) { Write-Output "[$($file.BaseName)] details cached, skip"; continue }
    $details = @()
    $n = 0
    foreach ($event in $job.trace) {
        if ($event.kind -ne "tool") { continue }
        $n++
        $url = "$Base/api/reviews/$($job.id)/traces/$($event.id)"
        $body = $null
        try { $body = curl.exe -s -m 15 $url } catch { }
        if ($body) {
            try { $details += ,@($event.id, ($body | ConvertFrom-Json)) } catch { }
        }
        Start-Sleep -Milliseconds 150
    }
    $merged = [ordered]@{ job_id = $job.id; case = $file.BaseName; fetched = (Get-Date -Format "o"); tool_events = $n; details = @{} }
    foreach ($pair in $details) { $merged.details[$pair[0]] = $pair[1] }
    $json = $merged | ConvertTo-Json -Depth 8
    [IO.File]::WriteAllText($outPath, $json, [Text.Encoding]::UTF8)
    Write-Output "[$($file.BaseName)] tool events=$n fetched=$($details.Count) -> $outPath"
}
Write-Output "DONE"
