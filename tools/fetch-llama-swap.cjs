const { execFileSync } = require('node:child_process');
const fs = require('node:fs');
const path = require('node:path');
const https = require('node:https');

const VER = process.argv.includes('--version') ? process.argv[process.argv.indexOf('--version') + 1] : '267';
const FB = path.join(__dirname, '..', 'internal', 'fb');
const TMP = path.join(__dirname, 'llama-swap_' + VER + '.zip');
const URLS = [
  'https://github.com/mostlygeek/llama-swap/releases/download/v' + VER + '/llama-swap_' + VER + '_windows_amd64.zip',
  'https://gh-proxy.com/https://github.com/mostlygeek/llama-swap/releases/download/v' + VER + '/llama-swap_' + VER + '_windows_amd64.zip',
  'https://cdn.jsdelivr.net/gh/mostlygeek/llama-swap@v' + VER + '/llama-swap_' + VER + '_windows_amd64.zip',
];

function curl(url, out) {
  try {
    execFileSync('curl.exe', ['-sL', '--connect-timeout', '25', '--max-time', '280', '--retry', '2', '-o', out, url], { stdio: 'inherit' });
  } catch {
    try { fs.rmSync(out, { force: true }); } catch {}
  }
  return fs.existsSync(out) ? fs.statSync(out).size : 0;
}

let ok = false;
for (const u of URLS) {
  for (let i = 1; i <= 2 && !ok; i++) {
    console.log('试 ' + u + ' 第 ' + i + ' 次');
    const sz = curl(u, TMP);
    if (sz > 5 * 1024 * 1024) { ok = true; console.log('下载 ' + sz + ' 字节'); break; }
  }
  if (ok) break;
}
if (!ok) { console.log('FETCH-BAD 三个源都没下到'); process.exit(1); }

const out = execFileSync('tar.exe', ['-tf', TMP], { encoding: 'utf8' });
const hit = out.split('\n').map((l) => l.trim()).find((l) => /(^|\/)llama-swap\.exe$/.test(l));
if (!hit) { console.log('FETCH-BAD 包里没找到 llama-swap.exe'); process.exit(1); }
execFileSync('tar.exe', ['-xf', TMP, '-C', path.dirname(TMP), hit], { stdio: 'inherit' });
const exe = path.join(path.dirname(TMP), hit.split('/').pop());
fs.copyFileSync(exe, path.join(FB, 'llama-swap.exe'));
fs.rmSync(exe, { force: true });
fs.rmSync(TMP, { force: true });
console.log('FETCH-OK 嵌入 ' + path.join(FB, 'llama-swap.exe') + '  ' + fs.statSync(path.join(FB, 'llama-swap.exe')).size + ' 字节（v' + VER + '）');
