const fs = require('node:fs');
const D = 'D:/06.AItools/forgeboard/cmd/forgeboard/frontend/dist/';
const i = fs.readFileSync(D + 'i18n.js', 'utf8');
const a = fs.readFileSync(D + 'app.js', 'utf8');
const c = fs.readFileSync(D + 'app.css', 'utf8');
const h = fs.readFileSync(D + 'index.html', 'utf8');
const out = [];
const k = i.indexOf('const ZH');
out.push('=== EN tail 300 ===\n' + i.slice(k - 300, k));
const s = a.indexOf('const before = SNAP');
out.push('=== selfTest lang ===\n' + a.slice(s, s + 520));
const d = a.indexOf("rep.push('tabsBackEN='");
out.push('=== tabsBack line ===\n' + a.slice(d - 40, d + 130));
const r = a.indexOf('function render(s)');
out.push('=== render ===\n' + a.slice(r, r + 200));
const cc = c.indexOf('.ck {');
out.push('=== css ck ===\n' + c.slice(cc, cc + 180));
const cnt = (src, t) => src.split(t).length - 1;
out.push('=== anchor counts ===');
for (const [name, src, t] of [
  ['html sessions hint', h, '<div class="hint" data-i18n="sessions_hint">'],
  ['html sys_engine_hint', h, '<div class="hint" data-i18n="sys_engine_hint">'],
  ['html diag buttons', h, '<button id="btnShot" class="btn ghost" data-i18n="btn_shot">Self-test</button>'],
  ['js render call', a, 'renderTop(s); renderGpus(s); renderModels(s); renderSystem(s);'],
  ['js boot shot bind', a, "$('btnShot').onclick = selfTest;"],
  ['js langSel onchange', a, "$('langSel').onchange = (e) => { setLang(e.target.value); if (SNAP) render(SNAP); };"],
  ['js boot LANG init', a, 'LANG = getLang();'],
  ['i18n windows_n EN', i, "  windows_n: 'window(s)',"],
  ['i18n windows_n ZH', i, "  windows_n: '个窗口',"],
  ['css .num', c, '.num { font-variant-numeric: tabular-nums; }'],
]) out.push(name + ' = ' + cnt(src, t));
fs.writeFileSync('D:/06.AItools/openclaw/workspace/tmp/data/anchors.txt', out.join('\n\n'), 'utf8');
console.log('ok');
