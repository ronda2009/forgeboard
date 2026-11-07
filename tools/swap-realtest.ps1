$ErrorActionPreference = 'Stop'
$dir = 'D:\06.AItools\forgeboard\v2'
$ctl = 8088                      # 测试专用管理端口（不占 8089，跟正式实例分开）
$cfg = Join-Path $dir 'tools\test-swap-config.json'
$log = Join-Path $dir 'tools\test-swap.log'

@'
{
  "$schema": "https://raw.githubusercontent.com/mostlygeek/llama-swap/main/config-schema.json",
  "startPort": 5900,
  "models": {
    "mxbai-embed-large": {
      "cmd": "D:\\06.AItools\\llama.cpp\\b11541\\llama-server.exe -m D:\\03.AImodels\\llama-router\\mxbai-embed-large.gguf -ngl 0 -c 512 --embeddings --host 127.0.0.1 --port ${PORT}",
      "name": "mxbai-embed-large (CPU test)"
    }
  }
}
'@ | Set-Content $cfg -Encoding utf8
& (Join-Path $dir 'llama-swap.exe') -config $cfg -validate
if ($LASTEXITCODE -ne 0) { throw '测试配置校验没过' }

Remove-Item $log -Force -ErrorAction SilentlyContinue
$p = Start-Process -FilePath (Join-Path $dir 'llama-swap.exe') -ArgumentList @('-config', $cfg, '-listen', "127.0.0.1:$ctl") `
     -WorkingDirectory $dir -WindowStyle Hidden -PassThru -RedirectStandardOutput $log -RedirectStandardError "$log.err"
Write-Host ("管理进程 pid=" + $p.Id)

$ok = $false
foreach ($i in 1..20) {
  Start-Sleep -Milliseconds 500
  try { if ((Invoke-WebRequest "http://127.0.0.1:$ctl/health" -UseBasicParsing -TimeoutSec 2).StatusCode -eq 200) { $ok = $true; break } } catch {}
}
Write-Host ("1) 管理进程就绪: " + $ok)
if (-not $ok) { Get-Content $log -ErrorAction SilentlyContinue | Select-Object -First 10; throw 'llama-swap 没就绪' }

$models = (Invoke-RestMethod "http://127.0.0.1:$ctl/v1/models" -TimeoutSec 5).data | ForEach-Object { $_.id }
Write-Host ("2) /v1/models 报的模型: " + ($models -join ', '))

$body = '{"model":"mxbai-embed-large","input":"hello forgeboard"}'
$sw = [Diagnostics.Stopwatch]::StartNew()
$emb = Invoke-RestMethod "http://127.0.0.1:$ctl/v1/embeddings" -Method Post -Body $body -ContentType 'application/json' -TimeoutSec 180
$sw.Stop()
Write-Host ("3) 起+首次推理成功，用时 " + [math]::Round($sw.Elapsed.TotalSeconds, 1) + " 秒，向量长度 " + $emb.data[0].embedding.Count)

$run = (Invoke-RestMethod "http://127.0.0.1:$ctl/running" -TimeoutSec 5) | ForEach-Object { $_.id }
Write-Host ("4) /running: " + ($run -join ', '))

Invoke-RestMethod "http://127.0.0.1:$ctl/api/models/unload" -Method Post -TimeoutSec 30 | Out-Null
Start-Sleep -Seconds 3
$run2 = (Invoke-RestMethod "http://127.0.0.1:$ctl/running" -TimeoutSec 5)
Write-Host ("5) unload 后 /running 条数: " + (@($run2).Count))

Stop-Process -Id $p.Id -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2
Write-Host ("6) 测试实例已停，8088 还在听: " + ((netstat -ano | Select-String ":$ctl.*LISTENING") | Measure-Object).Count)
Write-Host 'REALTEST-OK'
