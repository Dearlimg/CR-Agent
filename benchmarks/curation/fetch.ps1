# Fetch diffs and review comments for real-PR benchmark curation.
# Usage: powershell -File benchmarks/curation/fetch.ps1
# Diffs come from github.com (no API quota); comments from api.github.com
# (paginated, stops early if rate limit remaining < 5).
$ErrorActionPreference = "Stop"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path

$targets = @(
    @{ id = "ts-57465";      repo = "microsoft/TypeScript"; n = 57465 },
    @{ id = "node-41749";    repo = "nodejs/node";          n = 41749 },
    @{ id = "gin-2632";      repo = "gin-gonic/gin";        n = 2632 },
    @{ id = "pandas-27237";  repo = "pandas-dev/pandas";    n = 27237 },
    @{ id = "gin-2767";      repo = "gin-gonic/gin";        n = 2767 },
    @{ id = "redis-9323";    repo = "redis/redis";          n = 9323 },
    @{ id = "pandas-34473";  repo = "pandas-dev/pandas";    n = 34473 },
    @{ id = "tokio-4652";    repo = "tokio-rs/tokio";       n = 4652 },
    @{ id = "etcd-2323";     repo = "etcd-io/etcd";         n = 2323 },
    @{ id = "pandas-22862";  repo = "pandas-dev/pandas";    n = 22862 },
    @{ id = "tokio-6001";    repo = "tokio-rs/tokio";       n = 6001 },
    @{ id = "etcd-7221";     repo = "etcd-io/etcd";         n = 7221 },
    @{ id = "redis-9788";    repo = "redis/redis";          n = 9788 },
    @{ id = "gin-4224";      repo = "gin-gonic/gin";        n = 4224 },
    @{ id = "etcd-2009";     repo = "etcd-io/etcd";         n = 2009 }
)

function Get-RateRemaining {
    # Best-effort read of x-ratelimit-remaining from the last response headers.
    if (Test-Path "$dir/_headers.txt") {
        $line = (Get-Content "$dir/_headers.txt") | Where-Object { $_ -match "^x-ratelimit-remaining:" } | Select-Object -First 1
        if ($line) { return [int]($line -split ":")[1].Trim() }
    }
    return $null
}

$failed = @()
foreach ($t in $targets) {
    $id = $t.id; $repo = $t.repo; $n = $t.n
    $diffPath = Join-Path $dir "$id.diff"
    if (-not (Test-Path $diffPath) -or (Get-Item $diffPath).Length -eq 0) {
        curl.exe -sL "https://github.com/$repo/pull/$n.diff" -o $diffPath
    }
    $diffKB = if (Test-Path $diffPath) { [math]::Round((Get-Item $diffPath).Length / 1KB, 1) } else { 0 }
    Write-Output "[$id] diff=$diffKB KB"

    $commentsPath = Join-Path $dir "$id-comments.json"
    if (Test-Path $commentsPath) { continue }

    $all = @()
    $page = 1
    while ($true) {
        $remaining = Get-RateRemaining
        if ($null -ne $remaining -and $remaining -lt 5) {
            Write-Output "RATE LIMIT LOW ($remaining), stopping."; break
        }
        $url = "https://api.github.com/repos/$repo/pulls/$n/comments?per_page=100&page=$page"
        curl.exe -s -D "$dir/_headers.txt" -H "Accept: application/vnd.github+json" $url -o "$dir/_page.json"
        $items = @()
        try { $items = @((Get-Content "$dir/_page.json" -Raw | ConvertFrom-Json)) } catch { }
        if ($items.Count -eq 0) { break }
        $all += $items
        Write-Output "  page $page : $($items.Count) comments (remaining=$(Get-RateRemaining))"
        if ($items.Count -lt 100) { break }
        $page++
        Start-Sleep -Milliseconds 800
    }
    $all | ConvertTo-Json -Depth 12 | Set-Content -Path $commentsPath -Encoding UTF8
    Write-Output "[$id] total comments: $($all.Count)"
    Start-Sleep -Milliseconds 400
}
Remove-Item "$dir/_headers.txt","$dir/_page.json" -ErrorAction SilentlyContinue
Write-Output "DONE. Failed: $($failed.Count)"
