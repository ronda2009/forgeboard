const fs = require('fs');
const path = require('path');
const vm = require('vm');
const D = path.join(__dirname, '..', 'cmd', 'forgeboard', 'frontend', 'dist');
const read = (f) => fs.readFileSync(path.join(D, f), 'utf8');

const made = new Map();
function el(id) {
  if (!made.has(id)) {
    made.set(id, {
      id, textContent: '', innerHTML: '', checked: true, disabled: false, value: '', title: '',
      className: '', style: {}, children: [], classList: { toggle() {}, remove() {}, add() {} },
      querySelector() { return null; }, querySelectorAll() { return []; },
      insertAdjacentHTML(pos, html) { this.innerHTML += html; },
      appendChild(c) { this.innerHTML += (c && c.innerHTML) || ''; this.children.push(c); },
      closest() { return null; },
      setAttribute() {}, addEventListener() {},
    });
  }
  return made.get(id);
}
const sandbox = {
  document: {
    getElementById: el,
    addEventListener() {},
    createElement: () => el('tmp' + made.size),
    querySelectorAll: () => [],
    documentElement: el('html'),
  },
  window: {},
  navigator: { language: 'zh-CN' },
  localStorage: { getItem: () => null, setItem() {} },
  console,
  setTimeout,
  Date,
  Math,
  JSON,
  String,
  Number,
  Object,
  Array,
  RegExp,
  Intl,
};
sandbox.window = sandbox;
vm.createContext(sandbox);
vm.runInContext(read('i18n.js'), sandbox);
vm.runInContext(read('app.js'), sandbox);

const file = process.argv[2] || path.join(__dirname, '..', 'bin', 'openclaw-usage.json');
const usage = JSON.parse(fs.readFileSync(file, 'utf8'));
const snap = { usage, tasks: [], startup: [], selfDir: '', lang: 'zh' };

const checks = [];
const check = (name, ok, extra) => checks.push([name, !!ok, extra || '']);

sandbox.setLang('zh'); // 顶层 let LANG 不挂全局，只能走它自己的 setLang
sandbox.renderUsage(snap);

const stats = el('usStats').innerHTML;
const models = el('usModelRows').innerHTML;
const state = el('usState').textContent;
const chip = el('tokChip').textContent;
const unpriced = el('usUnpriced').textContent;
const rateNote = el('usRate').textContent;

check('three big number cards rendered', (stats.match(/class="us-stat/g) || []).length === 3, (stats.match(/class="us-stat/g) || []).length + ' cards');
check('today chip has a number', /\d/.test(chip), chip);
check('state line says when it was collected', /\d/.test(state) && state.length > 10, state.slice(0, 60));
check('model table has rows', (models.match(/<tr[ >]/g) || []).length >= 5, (models.match(/<tr[ >]/g) || []).length + ' rows');
check('model table has today/7d/30d columns', (models.match(/<td/g) || []).length >= (models.match(/<tr[ >]/g) || []).length * 4, '');
check('no window table left on the page', !made.has('usWindowRows') && !made.has('usMore'), made.has('usWindowRows') ? 'usWindowRows still touched' : 'gone');
const freeCells = (models.match(/class="us-num us-free"/g) || []).length;
check('local models are marked free', freeCells >= 5, freeCells + ' free cells');
const moneyCells = [...(models + stats).matchAll(/\u00a5([\d.]+)/g)].map((m) => m[1]);
check('paid models show yuan amounts', moneyCells.length >= 3, moneyCells.slice(0, 3).join(','));
check('no dollar sign left in the Chinese page', !/\$/.test(models + stats + rateNote), '');
check('yuan conversion is explained to the reader', /\u5143\/\u7f8e\u5143|yuan per US dollar/.test(rateNote), rateNote.slice(0, 60));
check('big card money is yuan too in Chinese', /\u00a5[\d.]+/.test(stats), stats.slice(0, 80));
check('unpriced note explains local models', /单价|unit price|未计价|not priced/.test(unpriced), unpriced.slice(0, 50));
check('no raw NaN or undefined leaked into the page', !/NaN|undefined/.test(stats + models + state + chip + unpriced), '');
const numCells = [...(stats + models).matchAll(/class="us-(tok|num|v)">([^<]*)</g)].map((m) => m[2]).join(' ');
check('token cells are shortened not raw digits', !/\d{7,}/.test(numCells), (numCells.match(/\d{7,}/g) || []).slice(0, 3).join(','));
check('Chinese page counts in 亿 and 万', /亿|万/.test(stats + models), '');
check('Chinese page has no B/M/k units left', !/[0-9.](B|M|k)\b/.test(numCells), (numCells.match(/[0-9.](B|M|k)\b/g) || []).slice(0, 3).join(','));
check('local models are labelled 本地部署', /本地部署/.test(models), '');
check('no 本机跑的 wording left in the usage panel', !/本机跑的/.test(models + unpriced + rateNote), '');

sandbox.setLang('en');
sandbox.renderUsage(snap);
const enStats = el('usStats').innerHTML;
const enModels = el('usModelRows').innerHTML;
check('English page keeps dollar amounts', /\$\d/.test(enModels) && !/¥/.test(enModels + enStats), '');
check('English page keeps B/M units and no Chinese units', /[0-9.](B|M)\b/.test(enStats) && !/亿|万/.test(enStats + enModels), '');
check('English free cell says local deployment', /free \(local deployment\)/.test(enModels), '');
check('English stats and model table have no Chinese', !/[一-鿿]/.test(enStats + enModels), '');
sandbox.setLang('zh');
sandbox.renderUsage(snap);

snap.tasks = [
  { name: 'mine', system: false, state: 'ready', trigger: 'BootTrigger', result: 'ok', lastRun: 0 },
  { name: 'windows-builtin', system: true, state: 'ready', trigger: 'BootTrigger', result: 'ok', lastRun: 0 },
];
el('ckTkSys').checked = true;
sandbox.renderTasks(snap);
const hidden = el('taskRows').innerHTML;
el('ckTkSys').checked = false;
sandbox.renderTasks(snap);
const shown = el('taskRows').innerHTML;
check('checkbox hides Windows built-in tasks', /mine/.test(hidden) && !/windows-builtin/.test(hidden), '');
check('unchecking shows all', /windows-builtin/.test(shown), '');
check('count line reports both numbers', /\d/.test(el('tkState').textContent), el('tkState').textContent.slice(0, 50));

const st1 = sandbox.stepText('step step_big_kill tier=iq3x ports=8095, 8096');
const st2 = sandbox.stepText('step step_svc_start port=8095 cmd=llama-server.exe');
check('progress line renders in the current language', /8095/.test(st1) && /iq3x/.test(st1) && !/step_/.test(st1), st1.slice(0, 50));
check('progress line fills both placeholders', /8095/.test(st2) && /llama-server\.exe/.test(st2) && !/\{/.test(st2), st2.slice(0, 50));

let bad = 0;
for (const [name, ok, extra] of checks) {
  console.log((ok ? 'PASS ' : 'FAIL ') + name + (extra ? ' :: ' + extra : ''));
  if (!ok) bad++;
}
console.log(bad ? 'FAIL ' + bad : 'PASS all ' + checks.length);
process.exit(bad ? 1 : 0);
