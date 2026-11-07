const fs = require('node:fs');
const p = process.argv[2];
const lines = fs.readFileSync(p, 'utf8').split('\n');
function bad(src) { try { new Function(src); return false; } catch (e) { return true; } }
if (!bad(lines.join('\n'))) { console.log('NO-ERROR'); process.exit(0); }
let lo = 0, hi = lines.length;
while (lo < hi - 1) {
  const mid = (lo + hi) >> 1;
  const src = lines.slice(0, mid).join('\n') + '\n' + lines.slice(mid).map(l => (l.match(/^\s*/) || [''])[0]).join('\n');
  if (bad(src)) hi = mid; else lo = mid;
}
console.log('first bad line ~' + (lo + 1) + ': ' + lines[lo].slice(0, 140));
for (let i = Math.max(0, lo - 3); i < Math.min(lines.length, lo + 3); i++) console.log((i + 1) + ': ' + lines[i].slice(0, 150));
