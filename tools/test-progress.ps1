param(
    [switch]$Race
)

$ErrorActionPreference = "Stop"
$goArgs = @("test", "-timeout=5m")
if ($Race) { $goArgs += @("-race", "-shuffle=on") }
$goArgs += "./..."

$mode = if ($Race) { "race" } else { "normal" }
$started = Get-Date
Write-Host "[LocalMesh] starting $mode suite; dependency compilation may be silent on a cold cache"
& go @goArgs
$code = $LASTEXITCODE
$elapsed = ((Get-Date) - $started).ToString("hh\:mm\:ss")
Write-Host "[LocalMesh] $mode suite finished in $elapsed (exit $code)"
exit $code
