param(
  [Parameter(Mandatory=$true)][string]$Out,
  [int]$Ticks = 12,
  [string]$ProcessName = 'forgeboard-gui'
)
$ErrorActionPreference = 'Continue'
Add-Type -AssemblyName System.Windows.Forms
Add-Type @"
using System; using System.Runtime.InteropServices;
public class W2 {
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int n);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out R r);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint dx, uint dy, int d, IntPtr e);
  public struct R { public int l, t, r, b; }
}
"@
$WHEEL = 0x0800
$p = Get-Process -Name $ProcessName -ErrorAction SilentlyContinue | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
if (-not $p) { Write-Output 'NO-WINDOW'; exit 1 }
[void][W2]::ShowWindow($p.MainWindowHandle, 9)
[void][W2]::SetForegroundWindow($p.MainWindowHandle)
Start-Sleep -Milliseconds 700
$rect = New-Object W2+R
[void][W2]::GetWindowRect($p.MainWindowHandle, [ref]$rect)
$cx = [int](($rect.l + $rect.r) / 2)
$cy = [int]($rect.t + ($rect.b - $rect.t) * 0.55)
[void][W2]::SetCursorPos($cx, $cy)
Start-Sleep -Milliseconds 400
for ($i = 0; $i -lt $Ticks; $i++) {
  [void][W2]::SetForegroundWindow($p.MainWindowHandle)
  [W2]::mouse_event($WHEEL, 0, 0, -600, [IntPtr]::Zero)
  Start-Sleep -Milliseconds 180
}
Write-Output "WHEEL-at $cx,$cy ticks=$Ticks"
Start-Sleep -Milliseconds 1500
& 'D:\06.AItools\openclaw\workspace\tools\window-shot.ps1' -ProcessName $ProcessName -Out $Out 2>&1 | Select-Object -First 3
