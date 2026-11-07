const fs = require('node:fs');
const V2 = 'D:/06.AItools/forgeboard/cmd/forgeboard/frontend/dist/';
const s = fs.readFileSync(V2 + 'i18n.js', 'utf8');
const start = s.indexOf('const EN =');
const end = s.indexOf('\n};', start);
const en = s.slice(start, end);
const out = [];
en.split('\n').forEach(l => {
  const m = /^\s*([a-z0-9_]+):\s*['"](.*)['"],?\s*$/.exec(l);
  if (m && /[\u4e00-\u9fff]/.test(m[2])) out.push('i18n ' + m[1] + ': ' + m[2].slice(0, 80));
});
const h = fs.readFileSync(V2 + 'index.html', 'utf8');
h.split('\n').forEach(l => {
  const m = /data-i18n="([a-z0-9_]+)">([^<]*)</.exec(l);
  if (m && /[\u4e00-\u9fff]/.test(m[2])) out.push('html ' + m[1] + ': ' + m[2].slice(0, 80));
});
console.log('en leftovers with Chinese: ' + out.length);
out.forEach(l => console.log(l));
