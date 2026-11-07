param(
  [int]$WaitSeconds = 150
)
$ErrorActionPreference = 'Continue'
Set-Location (Split-Path -Parent $PSScriptRoot)
Get-Process forgeboard-gui -ErrorAction SilentlyContinue | ForEach-Object { Stop-Process -Id $_.Id -Force }
Start-Sleep -Seconds 2
$launched = Get-Date
Start-Process -FilePath 'bin\forgeboard-gui.exe' -ArgumentList '--tab', 'debug' -WindowStyle Normal
$deadline = (Get-Date).AddSeconds($WaitSeconds)
$line = $null
while ((Get-Date) -lt $deadline) {
  $cand = Get-Content bin\forgeboard-gui.log -Encoding utf8 |
    Select-String -Pattern 'selftest tabs|boot-threw' | Select-Object -Last 1
  if ($cand) {
    $stampPrefix = $cand.Line.Substring(0, [Math]::Min(19, $cand.Line.Length))
    if ([string]::CompareOrdinal($stampPrefix, $launched.ToString('yyyy-MM-dd HH:mm:ss')) -ge 0) { $line = $cand; break }
  }
  Start-Sleep -Seconds 8
}
if (-not $line) { Write-Output 'NO LINE'; exit 1 }
$s = $line.Line
foreach ($k in @('ver=', 'versionRows', 'verSelf', 'startupRows', 'suBtns', 'suPills', 'taskRows', 'taskRes', 'tkState', 'tierBtns', 'tasksSecond', 'res2', 'tkState2', 'tierUp', 'curModel', 'selfOn', 'selfDir', 'cronCells', 'runModel', 'pinned', 'schedTxt', 'ocLink', 'extraEN', 'svcBtns', 'reserveMatch', 'hideWorks')) {
  $i = $s.IndexOf($k)
  if ($i -ge 0) { Write-Output ($k + ' -> ' + $s.Substring($i, [Math]::Min(95, $s.Length - $i))) }
  else { Write-Output ($k + ' -> (absent)') }
}
