# Merge redis-9788 review comments pages 2-4 into the existing JSON.
$ErrorActionPreference = "Stop"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path
$existing = @(Get-Content (Join-Path $dir "redis-9788-comments.json") -Raw -Encoding UTF8 | ConvertFrom-Json | Where-Object { $_.path })
Write-Output "existing valid comments: $($existing.Count)"

$all = $existing
foreach ($p in 2, 3, 4) {
    Start-Sleep -Seconds 8
    $rawPath = Join-Path $dir "_redis-p$p.json"
    curl.exe -s -o $rawPath "https://api.github.com/repos/redis/redis/pulls/9788/comments?per_page=100&page=$p"
    $bytes = (Get-Item $rawPath).Length
    $raw = [System.IO.File]::ReadAllText($rawPath, [System.Text.Encoding]::UTF8)
    $head = $raw.Trim().Substring(0, [Math]::Min(60, $raw.Trim().Length))
    Write-Output "page $p : bytes=$bytes head=$head"
    if (-not $raw.Trim().StartsWith("[")) { Write-Output "page $p is not an array, stopping."; break }
    $parsed = @(($raw | ConvertFrom-Json) | Where-Object { $_.path })
    Write-Output "page $p parsed: $($parsed.Count)"
    if ($parsed.Count -eq 0) { break }
    $all += $parsed
}
$all | ConvertTo-Json -Depth 12 | Set-Content (Join-Path $dir "redis-9788-comments.json") -Encoding UTF8
Write-Output "total=$($all.Count)"
Get-ChildItem $dir -Filter "_redis-p*.json" | Remove-Item
