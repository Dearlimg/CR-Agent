$ErrorActionPreference = "Stop"
$dir = Split-Path -Parent $MyInvocation.MyCommand.Path
foreach ($id in @("redis-9788", "etcd-2323", "ts-57465")) {
    $raw = Get-Content (Join-Path $dir "$id-comments.json") -Raw -Encoding UTF8
    Write-Output "== $id head: $($raw.Trim().Substring(0, 80) -replace "`n", ' ')"
    $o = $raw | ConvertFrom-Json
    $arr = @($o)
    Write-Output "   outer count=$($arr.Count)"
    for ($i = 0; $i -lt [Math]::Min(3, $arr.Count); $i++) {
        $e = $arr[$i]
        $t = $e.GetType().Name
        $ec = 0; try { $ec = @($e).Count } catch {}
        $eid = ""; try { $eid = $e.id } catch {}
        Write-Output "   elem[$i] type=$t innerCount=$ec id=$eid"
    }
}
