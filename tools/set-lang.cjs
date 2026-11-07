const fs = require('node:fs');
const want = (process.argv[2] || 'zh').toLowerCase();
const P = 'D:/06.AItools/forgeboard/bin/forgeboard.json';
const j = JSON.parse(fs.readFileSync(P, 'utf8'));
j.Lang = want;
fs.writeFileSync(P, JSON.stringify(j, null, 2), 'utf8');
console.log('Lang=' + JSON.parse(fs.readFileSync(P, 'utf8')).Lang);
