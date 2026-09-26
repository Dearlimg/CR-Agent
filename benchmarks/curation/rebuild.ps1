$ErrorActionPreference = "Stop"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path

function Normalize-Comments([string]$path) {
    $parsed = @(Get-Content $path -Raw -Encoding UTF8 | ConvertFrom-Json)
    $flat = @()
    foreach ($el in $parsed) {
        if ($null -ne $el.value -and $el.value -is [System.Array]) { $flat += @($el.value) }
        elseif ($el.id) { $flat += $el }
    }
    $seen = @{}; $uniq = @()
    foreach ($c in $flat) {
        $cid = $null; try { $cid = [int64]$c.id } catch { continue }
        if (-not $seen[$cid]) { $seen[$cid] = $true; $uniq += $c }
    }
    ConvertTo-Json -InputObject @($uniq) -Depth 12 | Set-Content $path -Encoding UTF8
    return $uniq
}

$redisPath = Join-Path $dir "redis-9788-comments.json"
$fixed = Normalize-Comments $redisPath
Write-Output "redis-9788 normalized: $($fixed.Count)"

$caseIds = @("ts-57465","node-41749","gin-2632","pandas-27237","gin-2767","redis-9323",
             "pandas-34473","tokio-4652","etcd-2323","pandas-22862","tokio-6001",
             "etcd-7221","gin-4224","etcd-2009")
$data = @{ "redis-9788" = $fixed }
foreach ($id in $caseIds) {
    $path = Join-Path $dir "$id-comments.json"
    if (-not (Test-Path $path)) { Write-Output "$id : MISSING"; $data[$id] = @(); continue }
    $u = Normalize-Comments $path
    $data[$id] = $u
    Write-Output "$id : $($u.Count) comments"
}

foreach ($id in @("ts-57465","node-41749","gin-2632","pandas-27237","gin-2767","redis-9323",
                  "pandas-34473","tokio-4652","etcd-2323","pandas-22862","tokio-6001",
                  "etcd-7221","redis-9788")) {
    $rows = @()
    $idx = 0
    foreach ($c in $data[$id]) {
        $anchored = ($null -ne $c.position) -or ($null -ne $c.original_position)
        if ($anchored -and $c.body.Length -ge 40) {
            $line = if ($null -ne $c.original_line) { $c.original_line } else { $c.line }
            $rows += [pscustomobject]@{
                idx = $idx; cid = $c.id; author = $c.user.login; path = $c.path; line = $line
                side = $c.side; subj = $c.subject_type; reply = [bool]$c.in_reply_to_id
                bodylen = $c.body.Length; body = ($c.body -replace "`r", "")
            }
        }
        $idx++
    }
    $rows = $rows | Sort-Object path, @{ Expression = { [int]$_.line } }, idx
    $sb = New-Object System.Text.StringBuilder
    [void]$sb.AppendLine("# digest $id : $($rows.Count) anchored candidates (of $($data[$id].Count) total)")
    foreach ($r in $rows) {
        [void]$sb.AppendLine()
        [void]$sb.AppendLine("## [$($r.idx)] $($r.path):$($r.line) author=$($r.author) reply=$($r.reply) len=$($r.bodylen) subj=$($r.subj) side=$($r.side) cid=$($r.cid)")
        [void]$sb.AppendLine($r.body)
    }
    Set-Content -Path (Join-Path $dir "$id-digest.md") -Value $sb.ToString() -Encoding UTF8
    Write-Output "$id : digest rows=$($rows.Count)"
}
Write-Output "DIGESTS DONE"
