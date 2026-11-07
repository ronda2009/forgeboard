const fs = require('fs');
for (const p of process.argv.slice(2)) {
  try {
    const b = fs.readFileSync(p);
    const pe = b.readUInt32LE(0x3c);
    const machine = b.readUInt16LE(pe + 4);
    const opt = pe + 24;
    const magic = b.readUInt16LE(opt);
    const subsystem = b.readUInt16LE(opt + 68);
    const name = p.replace(/.*[\\/]/, '');
    console.log(name + '  magic=' + magic.toString(16) + '  subsystem=' + subsystem + (subsystem === 2 ? ' (GUI 无黑框)' : subsystem === 3 ? ' (控制台 有黑框)' : ' (?)') + '  machine=' + machine.toString(16));
  } catch (e) {
    console.log(p + '  ERR ' + e.message);
  }
}
