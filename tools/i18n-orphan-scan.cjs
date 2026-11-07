const fs = require('fs');
const d = 'D:/06.AItools/forgeboard/cmd/forgeboard/frontend/dist/';
const i18n = fs.readFileSync(d + 'i18n.js', 'utf8');
const app = fs.readFileSync(d + 'app.js', 'utf8');
const html = fs.readFileSync(d + 'index.html', 'utf8');
const hay = app + ' ' + html;
const DYN = ['op_', 'st_', 'kind_', 'w_', 'cal_', 'role_', 'col_', 'note_', 'svc_', 'where_', 'act_'];
const en = i18n.split('const ZH')[0];
const keys = [...en.matchAll(/^  ([a-z0-9_]+):/gm)].map(m => m[1]);
const used = (k) => hay.indexOf("'" + k + "'") >= 0 || hay.indexOf('"' + k + '"') >= 0 || hay.indexOf('data-i18n="' + k + '"') >= 0 ||
  DYN.some(p => k.startsWith(p) && hay.indexOf("'" + p + "'") >= 0);
const orphans = keys.filter(k => !used(k));
console.log('keys=' + keys.length + ' orphan=' + orphans.length);
console.log(orphans.join(', '));
