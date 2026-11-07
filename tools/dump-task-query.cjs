const fs = require('fs');
const src = fs.readFileSync('D:/06.AItools/forgeboard/internal/fb/tasks_windows.go', 'utf8');
const key = 'const suTaskQuery = `';
const i = src.indexOf(key);
if (i < 0) { console.log('MISS suTaskQuery'); process.exit(1); }
const start = i + key.length;
const end = src.indexOf('`', start);
if (end < 0) { console.log('MISS closing backtick'); process.exit(1); }
const body = src.slice(start, end);
const out = 'D:/06.AItools/openclaw/workspace/tmp/scripts/probe-t089-tasks3.ps1';
fs.writeFileSync(out, body, 'utf8');
console.log('wrote ' + out + ' bytes=' + body.length);
