const fs = require('fs');

const [src, dst] = process.argv.slice(2);
if (!src || !dst) { console.log('用法: node tools/extract-manifest.cjs <旧.syso> <出.manifest>'); process.exit(1); }

const buf = fs.readFileSync(src);

function rsrcStart() {
  const nSec = buf.readUInt16LE(2);
  let p = 20;
  for (let i = 0; i < nSec; i++) {
    let name = buf.toString('latin1', p, p + 8).replace(/\0/g, '');
    if (name.startsWith('/')) {
      const strOff = parseInt(name.slice(1), 10);
      const s = buf.toString('latin1', p + 20, buf.indexOf(0, p + 20) < 0 ? buf.length : buf.indexOf(0, p + 20));
      let acc = 0, j = 0;
      while (j < s.length && acc < strOff) { acc++; const z = s.indexOf('\0', j); j = z < 0 ? s.length : z + 1; }
      const z2 = s.indexOf('\0', j);
      name = s.slice(j, z2 < 0 ? s.length : z2).replace(/\0/g, '');
    }
    const rawSize = buf.readUInt32LE(p + 16);
    const rawPtr = buf.readUInt32LE(p + 20);
    if (name === '.rsrc') return { ptr: rawPtr, size: rawSize };
    p += 40;
  }
  return null;
}

const loc = rsrcStart();
if (!loc) { console.log('这个文件里没有 .rsrc 节'); process.exit(1); }
const start = loc.ptr;

function readDir(o) {
  const named = buf.readUInt16LE(o + 12);
  const ids = buf.readUInt16LE(o + 14);
  const out = [];
  let p = o + 16;
  for (let i = 0; i < named + ids; i++, p += 8) {
    const nameField = buf.readUInt32LE(p);
    const off = buf.readUInt32LE(p + 4);
    out.push({ id: nameField & 0x7fffffff, off: off & 0x7fffffff, dir: !!(off & 0x80000000) });
  }
  return out;
}

const top = readDir(start);
const man = top.find(e => e.id === 24 && e.dir);
if (!man) { console.log('旧 syso 里没有清单（那新 syso 也不用带）'); process.exit(2); }
const names = readDir(start + man.off);
if (!names.length) { console.log('清单类型目录是空的'); process.exit(1); }
const langs = readDir(start + names[0].off);
if (!langs.length) { console.log('清单名字目录是空的'); process.exit(1); }
const dataOff = buf.readUInt32LE(start + langs[0].off);
const size = buf.readUInt32LE(start + langs[0].off + 4);
if (dataOff === 0 || size === 0 || start + dataOff + size > buf.length) {
  console.log('清单数据位置不对（off=' + dataOff + ' size=' + size + '）'); process.exit(1);
}
const xml = buf.toString('utf8', start + dataOff, start + dataOff + size);
if (!xml.includes('<assembly')) { console.log('抠出来的内容不像清单 XML'); process.exit(1); }
fs.writeFileSync(dst, xml);
console.log('OK 抠出清单 ' + size + ' 字节 → ' + dst);
console.log(xml.slice(0, 220).replace(/\r?\n/g, ' '));
