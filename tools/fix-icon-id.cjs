const fs = require('fs');

const file = process.argv[2];
const want = Number(process.argv[3] || 3);
if (!file) {
  console.log('用法: node tools/fix-icon-id.cjs <文件.syso> [新编号=3]');
  process.exit(1);
}
const buf = fs.readFileSync(file);

const nSec = buf.readUInt16LE(2);
let sec = 20, rsrc = -1;
for (let i = 0; i < nSec; i++) {
  const name = buf.toString('latin1', sec, sec + 8).replace(/\0/g, '');
  if (name === '.rsrc') rsrc = buf.readUInt32LE(sec + 20);
  sec += 40;
}
if (rsrc < 0) {
  console.log('这个文件里没有 .rsrc 节');
  process.exit(1);
}

const named = buf.readUInt16LE(rsrc + 12);
const ids = buf.readUInt16LE(rsrc + 14);
let p = rsrc + 16 + named * 8;
const RT_GROUP_ICON = 14;
let hit = -1;
for (let i = 0; i < ids; i++, p += 8) {
  if (buf.readUInt32LE(p) === RT_GROUP_ICON) {
    hit = buf.readUInt32LE(p + 4) & 0x7fffffff;
    break;
  }
}
if (hit < 0) {
  console.log('没有 RT_GROUP_ICON(14) 类型');
  process.exit(1);
}

const dir = rsrc + hit;
const dNamed = buf.readUInt16LE(dir + 12);
const dIds = buf.readUInt16LE(dir + 14);
const e0 = dir + 16 + dNamed * 8;
if (dIds < 1) {
  console.log('组图标目录是空的');
  process.exit(1);
}
const old = buf.readUInt32LE(e0);
buf.writeUInt32LE(want, e0);
fs.writeFileSync(file, buf);
console.log(`组图标编号 ${old} → ${want}（条目 ${dIds} 个，文件 ${buf.length} B）`);
