const fs = require('fs');

function dirs(buf, start, limit) {
  const out = [];
  const read = (o) => {
    if (o + 16 > buf.length) return null;
    const named = buf.readUInt16LE(o + 12);
    const ids = buf.readUInt16LE(o + 14);
    const entries = [];
    let p = o + 16;
    for (let i = 0; i < named + ids && p + 8 <= buf.length && entries.length < limit; i++, p += 8) {
      const nameField = buf.readUInt32LE(p);
      const off = buf.readUInt32LE(p + 4);
      let name = null;
      if (nameField & 0x80000000) {
        const so = start + (nameField & 0x7fffffff);
        const len = buf.readUInt16LE(so);
        name = buf.toString('utf16le', so + 2, so + 2 + len * 2);
      }
      entries.push({ id: nameField & 0x7fffffff, name, off: off & 0x7fffffff, dir: !!(off & 0x80000000) });
    }
    return entries;
  };
  const top = read(start);
  if (!top) return out;
  for (const e of top) {
    const item = { type: e.id, id: e.name === null ? e.id : e.name, name: e.name };
    if (e.dir) {
      item.sub = read(start + e.off) || [];
    }
    out.push(item);
  }
  return out;
}

function rsrcStart(buf) {
  if (buf.readUInt16LE(0) === 0x5a4d) {
    const pe = buf.readUInt32LE(0x3c);
    const nSec = buf.readUInt16LE(pe + 6);
    const optSize = buf.readUInt16LE(pe + 20);
    let p = pe + 24 + optSize;
    for (let i = 0; i < nSec; i++) {
      const name = buf.toString('latin1', p, p + 8).replace(/\0/g, '');
      const rawSize = buf.readUInt32LE(p + 16);
      const rawPtr = buf.readUInt32LE(p + 20);
      if (name === '.rsrc') return { ptr: rawPtr, size: rawSize };
      p += 40;
    }
    return null;
  }
  const nSec = buf.readUInt16LE(2);
  let p = 20;
  for (let i = 0; i < nSec; i++) {
    let name = buf.toString('latin1', p, p + 8).replace(/\0/g, '');
    if (name.startsWith('/')) {
      const strOff = parseInt(name.slice(1), 10);
      const end = buf.indexOf(0, p + 20 + 0);
      let s = buf.toString('latin1', p + 20, end < 0 ? buf.length : end);
      let acc = 0, j = 0;
      while (j < s.length && acc < strOff) { acc++; const z = s.indexOf('\0', j); j = z < 0 ? s.length : z + 1; }
      name = s.slice(j, s.indexOf('\0', j) < 0 ? s.length : s.indexOf('\0', j)).replace(/\0/g, '');
    }
    const rawSize = buf.readUInt32LE(p + 16);
    const rawPtr = buf.readUInt32LE(p + 20);
    if (name === '.rsrc') return { ptr: rawPtr, size: rawSize };
    p += 40;
  }
  return null;
}

const TYPES = { 3: 'RT_ICON', 14: 'RT_GROUP_ICON', 16: 'RT_VERSION', 24: 'RT_MANIFEST' };
for (const f of process.argv.slice(2)) {
  let buf;
  try { buf = fs.readFileSync(f); } catch (e) { console.log(f, '读不到', e.message); continue; }
  const loc = rsrcStart(buf);
  console.log('===', f, buf.length, 'B', loc ? `.rsrc@${loc.ptr} (${loc.size}B)` : '没有 .rsrc');
  if (!loc) continue;
  for (const t of dirs(buf, loc.ptr, 40)) {
    const tn = TYPES[t.type] || ('type' + t.type);
    const subs = (t.sub || []).map((s) => (s.name ? `"${s.name}"` : s.id));
    console.log(`  ${tn}: 条目 ${subs.length} 个 → [${subs.join(', ')}]`);
  }
}
