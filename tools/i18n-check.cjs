const fs = require('fs');
const path = require('path');
const D = path.join(__dirname, '..', 'cmd', 'forgeboard', 'frontend', 'dist');

const i18n = fs.readFileSync(path.join(D, 'i18n.js'), 'utf8');
const app = fs.readFileSync(path.join(D, 'app.js'), 'utf8');
const html = fs.readFileSync(path.join(D, 'index.html'), 'utf8');

const cut = i18n.indexOf('const ZH');
if (cut < 0) { console.log('FAIL no "const ZH" in i18n.js - check the dictionary variable names'); process.exit(1); }
const en = i18n.slice(0, cut);
const zh = i18n.slice(cut);

function keys(s) {
  const out = new Set();
  const re = /^\s*([a-z0-9_]+):/gm;
  let m;
  while ((m = re.exec(s))) out.add(m[1]);
  return out;
}

const ke = keys(en), kz = keys(zh);
const used = new Set();
const pats = [/\bt\(\s*'([a-z0-9_]+)'/g, /\bt\(\s*"([a-z0-9_]+)"/g, /data-i18n(?:-title)?="([a-z0-9_]+)"/g];
for (const re of pats) { let m; while ((m = re.exec(app))) used.add(m[1]); }
for (const re of [/data-i18n(?:-title)?="([a-z0-9_]+)"/g]) { let m; while ((m = re.exec(html))) used.add(m[1]); }

const goDir = path.join(__dirname, '..', 'internal');
const goTokens = new Set();
const stepTokens = new Set();
function walk(dir) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name);
    if (e.isDirectory()) { walk(p); continue; }
    if (!e.name.endsWith('.go')) continue;
    const src = fs.readFileSync(p, 'utf8');
    let m;
    for (const re of [/\bop\("([a-z0-9_]+)"/g, /\bStep\("(step_[a-z0-9_]+)"/g]) {
      while ((m = re.exec(src))) (re.source.indexOf("Step") >= 0 ? stepTokens : goTokens).add(m[1]);
    }
  }
}
walk(goDir);

const zhMissing = [...ke].filter((k) => !kz.has(k));
const enMissing = [...kz].filter((k) => !ke.has(k));
const goMissing = [...goTokens].filter((t) =>
  t.endsWith('_') ? ![...ke].some((k) => k.startsWith('op_' + t)) : !ke.has('op_' + t));
const stepMissing = [...stepTokens].filter((k) => !ke.has(k) || !kz.has(k));
const unused = [...ke].filter((k) =>
  !used.has(k) && !k.startsWith('op_') && !k.startsWith('note_') && !k.startsWith('step_') && !goTokens.has(k.slice(3)) &&
  !/^(st_|tk_|su_src_|ver_|op_)/.test(k));

const { execFileSync } = require('child_process');
let syn = 'syntax-ok';
for (const f of ['i18n.js', 'app.js']) {
  try { execFileSync(process.execPath, ['--check', path.join(D, f)], { stdio: 'pipe' }); } catch (e) { syn = 'SYNTAX-FAIL ' + f; }
}

console.log('js-syntax=' + syn);
console.log('en=' + ke.size + ' zh=' + kz.size + ' ui-used=' + used.size + ' go-tokens=' + goTokens.size);
console.log('zh-missing=' + (zhMissing.length ? zhMissing.join(',') : 'none'));
console.log('en-missing=' + (enMissing.length ? enMissing.join(',') : 'none'));
console.log('go-token-missing-in-dict=' + (goMissing.length ? goMissing.join(',') : 'none'));
console.log('step-tokens=' + stepTokens.size + ' step-missing-in-dict=' + (stepMissing.length ? stepMissing.join(',') : 'none'));
console.log('unused-keys(possible dynamic)=' + unused.length + (unused.length ? ' :: ' + unused.slice(0, 15).join(',') : ''));
const bad = zhMissing.length || enMissing.length || goMissing.length || stepMissing.length;
console.log(bad ? 'FAIL' : 'PASS');
process.exit(bad ? 1 : 0);
