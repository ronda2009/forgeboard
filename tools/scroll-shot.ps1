param(
  [Parameter(Mandatory=$true)][string]$ProcessName,
  [Parameter(Mandatory=$true)][string]$Out,
  [int]$Clicks = 6
)
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System; using System.Runtime.InteropServices;
public class W {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out R r);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint x, uint y, uint d, IntPtr e);
  public struct R { public int l, t, r, b; }
}
"@
$DOWN = 0x0002; $UP = 0x0004
$p = Get-Process -Name $ProcessName -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
if (-not $p) { Write-Output 'NO-WINDOW'; exit 1 }
[void][W]::ShowWindow($p.MainWindowHandle, 9)   # SW_9 = restore
[void][W]::SetForegroundWindow($p.MainWindowHandle)
Start-Sleep -Milliseconds 700
$rect = New-Object W+R
[void][W]::GetWindowRect($p.MainWindowHandle, [ref]$rect)
$cx = [int](($rect.l + $rect.r) / 2)
$cy = [int]($rect.t + ($rect.b - $rect.t) * 0.45)   # 页面中部（避开标题栏与顶部标签条）
try {
  [void][W]::SetForegroundWindow($p.MainWindowHandle)
  [void][W]::SetCursorPos($cx, $cy); Start-Sleep -Milliseconds 200
  [W]::mouse_event($DOWN, 0, 0, 0, [IntPtr]::Zero)
  [W]::mouse_event($UP, 0, 0, 0, [IntPtr]::Zero)
  Start-Sleep -Milliseconds 500
  for ($i = 0; $i -lt $Clicks; $i++) {
    [void][W]::SetForegroundWindow($p.MainWindowHandle)
    [System.Windows.Forms.SendKeys]::SendWait("{PGDN}")
    Start-Sleep -Milliseconds 300
  }
  Write-Output "CLICKED-at $cx,$cy"
} catch { Write-Output "SENDKEYS-FAILED $($_.Exception.Message)" }
Start-Sleep -Milliseconds 1500
& 'D:\06.AItools\openclaw\workspace\tools\window-shot.ps1' -ProcessName $ProcessName -Out $Out 2>&1 | Select-Object -First 2
