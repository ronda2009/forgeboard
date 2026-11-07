const fs = require('fs');
const path = require('path');
const D = path.join(__dirname, '..', 'cmd', 'forgeboard', 'frontend', 'dist');
const read = (f) => fs.readFileSync(path.join(D, f), 'utf8');

const app = read('app.js');
const html = read('index.html');
const i18n = read('i18n.js');

const US_IDS = [
  'tokChip', 'usState', 'usStats', 'usModelRows', 'usUnpriced',
  'btnUsageNow', 'ckTkSys', 'stepOut', 'usRate',
];

function fmtTokBlock(src) {
  const a = src.indexOf('function fmtTok(');
  const b = src.indexOf('const US_RATE_FALLBACK');
  return a < 0 || b < 0 ? '' : src.slice(a, b);
}

function fmtCostBlock(src) {
  const i = src.indexOf('function fmtCost(');
  return i < 0 ? '' : src.slice(i, src.indexOf('function usAge(', i));
}
function rateLiterals(src) {
  return (src.match(/\b7\.1\b/g) || []);
}

const checks = [
  ['index.html has the reload button', /id="btnReloadSwap"/.test(html)],
  ['index.html has the entry line', /id="entryLine"/.test(html)],
  ['app.js wires the reload button', /\$\('btnReloadSwap'\)\.onclick\s*=/.test(app)],
  ['app.js calls ReloadSwap (dry then real)', /goApp\(\)\.ReloadSwap\(true\)/.test(app) && /goApp\(\)\.ReloadSwap\(false\)/.test(app)],
  ['app.js renders the entry line from the snapshot', /function renderEntry\(s\)/.test(app) && /renderEntry\(s\)/.test(app)],
  ['entry line uses the real swap port, not a literal', /s\.swap && s\.swap\.port/.test(app)],
  ['start/stop results go through opText', /opText\(await goApp\(\)\.StartModel\(key, true\)\)/.test(app) && /opText\(await goApp\(\)\.StartModel\(key, false\)\)/.test(app)],
  ['self-test exercises the reload dry run', /reloadDry/.test(app) && /goApp\(\)\.ReloadSwap\(true\)/.test(app)],
  ['self-test reports the entry line', /entryMatchesReal/.test(app) && /entryHasOld8090/.test(app)],
  ['no hardcoded 8090 in HTML or in dictionary sentences', !/8090/.test(html) && !/8090/.test(i18nSentences(i18n))],
  ['app.js mentions 8090 only inside self-test reporting', bad8090Lines(app).length === 0],
  ['reload/entry keys exist in both dictionaries', hasBoth(i18n, 'btn_reload_swap') && hasBoth(i18n, 'entry_label') && hasBoth(i18n, 'entry_none') && hasBoth(i18n, 'confirm_title_reload')],
  ['index.html has the usage panel ids', US_IDS.every((id) => html.includes('id="' + id + '"'))],
  ['app.js touches every usage id', US_IDS.every((id) => app.includes("$('" + id + "')"))],
  ['app.js renders the usage panel from the snapshot', /function renderUsage\(s\)/.test(app) && /renderUsage\(s\)/.test(app)],
  ['usage panel reads the snapshot, never the gateway', !/goApp\(\)/.test(app.slice(app.indexOf('function renderUsage'), app.indexOf('function renderStartup')))],
  ['collect-now starts one round and does not block', /function doUsageNow\(\)/.test(app) && /goApp\(\)\.UsageNow\(\)/.test(app)],
  ['task table hides Windows built-ins on the checkbox', /ckTkSys/.test(app) && /!r\.system/.test(app)],
  ['progress lines render through the dictionary', /function stepText\(msg\)/.test(app) && /t\(key\)/.test(app)],
  ['progress lines land on their own panel', app.includes("$('stepOut')")],
  ['usage and step keys exist in both dictionaries', ['us_box', 'us_now', 'us_state', 'us_chip', 'us_free', 'us_col_today', 'us_col_7d', 'us_col_30d', 'us_local_free', 'us_rate_note', 'us_rate_fallback', 'tk_hide_sys', 'step_big_kill', 'step_svc_start', 'step_gw_wait'].every((k) => hasBoth(i18n, k))],
  ['usage panel shows the three ranges and per-model totals only', /usStats/.test(app) && /usModelRows/.test(app) && !/usDailyRows|usGroupRows/.test(app)],
  ['local models render as free', /m\.local/.test(app) && /us_local_free/.test(app)],
  ['cost converts to yuan in Chinese using the collected rate', /LANG === 'zh'/.test(fmtCostBlock(app)) && /u\.rate/.test(app) && /us_rate_note/.test(app)],
  ['no hardcoded exchange rate outside the fallback', rateLiterals(app).length === 1, rateLiterals(app).join(',')],
  ['Chinese token units are yi and wan', /亿/.test(app) && /万/.test(app) && /LANG === 'zh'/.test(fmtTokBlock(app))],
  ['free cell wording says local deployment in both languages', /local deployment/.test(i18n) && /本地部署/.test(i18n) && !/本机跑的/.test(i18n)],
  ['window table is gone from HTML and app.js', !/usWindowRows|us_window_head|us_col_window|usMore/.test(html) && !/usWindowRows|us_kind_|us_other_windows/.test(app)],
];

function i18nSentences(s) {
  return s.split('\n').filter((l) => /^\s*[a-z0-9_]+:/.test(l)).join('\n');
}

function bad8090Lines(s) {
  return s
    .split('\n')
    .filter((l) => l.includes('8090'))
    .filter((l) => !/^\s*\/\//.test(l))
    .filter((l) => !/rep\.push|entryHasOld8090|svcStartDry/.test(l));
}

function hasBoth(s, key) {
  const cut = s.indexOf('const ZH');
  const re = new RegExp('^\\s*' + key + ':', 'm');
  return re.test(s.slice(0, cut)) && re.test(s.slice(cut));
}

let bad = 0;
for (const [name, ok] of checks) {
  console.log((ok ? 'PASS ' : 'FAIL ') + name);
  if (!ok) bad++;
}
console.log(bad ? 'FAIL ' + bad : 'PASS all ' + checks.length);
process.exit(bad ? 1 : 0);
