const fs = require('node:fs');
const s = fs.readFileSync('D:/06.AItools/forgeboard/cmd/forgeboard/frontend/dist/i18n.js', 'utf8');
const start = s.indexOf('const EN =');
const en = s.slice(start, s.indexOf('\n};', start));
const flags = [];
en.split('\n').forEach(l => {
  const m = /^\s*([a-z0-9_]+):\s*'(.*)',$/.exec(l);
  if (!m) return;
  const k = m[1], v = m[2];
  const hits = [];
  if (/\b(can't|don't|won't|it's|you're|isn't|doesn't|there's)\b/i.test(v)) hits.push('缩写词');
  if (/!/.test(v)) hits.push('感叹号');
  if (/\b(ok|okay|yeah|stuff|kinda|gonna|ASAP|etc)\b/i.test(v)) hits.push('口语词');
  if (/^[a-z]/.test(v) && !/^(this|the|a|an|no|not|if|when|only|one|every|idle|card|first|other|remote|local|nothing|reading|writing|switch|set|clear|restart|roll|save|run|measure|unload|models|sessions|services|system|diagnostics|jobs|heartbeats|gateway|router|provider|engine|ports|paths|window|power|reserve|cache|version|what|where|used|state|action|model|needs|port|installed|note|role|running|tier|file)\b/.test(v)) hits.push('小写开头');
  if (/\bclick\b|\bpls\b|\bkindly\b/i.test(v)) hits.push('措辞');
  if (hits.length) flags.push(k + '  [' + hits.join(' ') + ']  ' + v.slice(0, 100));
});
console.log('flagged: ' + flags.length);
flags.forEach(f => console.log(f));
