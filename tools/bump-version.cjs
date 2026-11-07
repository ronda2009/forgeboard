const fs = require('fs');
const F = 'D:/06.AItools/forgeboard/internal/cli/cli.go';
const want = process.argv[2];
if (!want) { console.log('usage: node bump-version.cjs <version>'); process.exit(1); }
let s = fs.readFileSync(F, 'utf8');
const re = /Version\s*=\s*"([^"]+)"/;
const m = s.match(re);
if (!m) { console.log('MISS version const'); process.exit(1); }
console.log('was: ' + m[1]);
const n = s.split(m[0]).length - 1;
if (n !== 1) { console.log('MISS hits=' + n); process.exit(1); }
s = s.split(m[0]).join('Version = "' + want + '"');
fs.writeFileSync(F, s, 'utf8');
console.log('now: ' + (s.match(re) || ['?', '?'])[1]);
