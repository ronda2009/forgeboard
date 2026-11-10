let SNAP = null;
let TAB = 'models';

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s == null ? '' : s).replace(/[&<>"]/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c]));

function goApp() {
  if (!(window.go && window.go.app && window.go.app.App)) throw new Error(t('err_prefix') + 'bridge not ready');
  return window.go.app.App;
}

function feed(msg) {
  if (stepKey(msg)) { stepLine(msg); return; }
  hideStepBar(); // 动作有了结论，进度条让位
  const out = $('feedOut');
  out.textContent = (out.textContent + '\n' + msg).split('\n').slice(-40).join('\n');
}

function stepKey(msg) {
  const s = String(msg).replace(/^\[frontend\]\s*/, '');
  const m = s.match(/^step\s+(step_\S+)/);
  return m ? m[1] : null;
}
function stepArgs(msg) {
  const s = String(msg).replace(/^\[frontend\]\s*/, '');
  const hits = [];
  const re = /\s(\w+)=/g;
  let m;
  while ((m = re.exec(s)) !== null) hits.push([m[1], m.index]);
  const a = {};
  hits.forEach(function (kv, idx) {
    const start = kv[1] + kv[0].length + 2;
    const end = idx + 1 < hits.length ? hits[idx + 1][1] : s.length;
    a[kv[0]] = s.slice(start, end).trim();
  });
  return a;
}
function stepText(msg) {
  const key = stepKey(msg);
  if (!key) return String(msg);
  const a = stepArgs(msg);
  let s = t(key);
  Object.keys(a).forEach(k => { s = s.split('{' + k + '}').join(a[k]); });
  return s;
}
function stepLine(msg) {
  const txt = stepText(msg);
  const out = $('stepOut');
  if (!out) { feed(msg); return; }
  out.textContent = (out.textContent + '\n' + txt).split('\n').slice(-60).join('\n');
  const ev = $('feedOut'); // 事件日志里也留一行：重启这类动作的每一步都要查得到
  if (ev) ev.textContent = (ev.textContent + '\n' + txt).split('\n').slice(-40).join('\n');
  showStepBar(txt); // 在别的页面上也能看到「进行到哪一步」
}

function showStepBar(txt) {
  if (document.body.classList.contains('selftesting')) return;
  const bar = $('busyBar');
  if (!bar) return;
  bar.textContent = txt;
  bar.classList.remove('hidden');
}
function hideStepBar() {
  const bar = $('busyBar');
  if (bar && !document.body.classList.contains('selftesting')) bar.classList.add('hidden');
}

function miB(n) {
  if (!n) return '0';
  return n >= 1024 ? (n / 1024).toFixed(1) + ' GiB' : n + ' MiB';
}

function renderTop(s) {
  $('brandVer').textContent = s.stamp || s.version;
  const rt = s.router || {};
  const tiers = rt.tiers || [];
  const tier = tiers.find(x => rt.current && x.tier === rt.current);
  const curStopped = !!rt.current && !!tier && !tier.running;
  $('curModel').textContent = !rt.current ? (t('cur_model') + ': ' + t('cur_none'))
    : (t('cur_model') + ': ' + rt.current + (curStopped ? ' · ' + t('cur_model_stopped') : ''));
  const mon = $('monLink');
  const svc = (rt.service || '').replace(/^[a-z]/, c => c.toUpperCase());
  const monTxt = (svc ? svc + ' ' : '') + t('mon_link');
  if (tier && tier.port) {
    const url = 'http://127.0.0.1:' + tier.port + '/';
    mon.textContent = monTxt;
    mon.title = url;
    mon.onclick = () => goApp().OpenLink(url).catch(e => feed(t('err_prefix') + (e.message || e)));
  } else {
    mon.textContent = t('mon_none');
    mon.title = rt.url || '';
    mon.onclick = null;
  }
const oc = $("ocLink");
if (s.gwPort) {
  const ocUrl = "http://127.0.0.1:" + s.gwPort + "/";
  oc.textContent = t("oc_link");
  oc.title = ocUrl;
  oc.onclick = () => goApp().OpenLink(ocUrl).catch(e => feed(t("err_prefix") + (e.message || e)));
} else {
  oc.textContent = t("oc_none");
  oc.title = t("oc_none_hint");
  oc.onclick = null;
}
  renderRefreshAt();
  const g = s.gpus || [];
  if (g.length) {
    const free = g.reduce((a, x) => a + x.free, 0), tot = g.reduce((a, x) => a + x.total, 0);
    $('vramChip').textContent = tr('Free', 'vram_free') + ' ' + miB(free) + ' / ' + miB(tot);
  } else {
    $('vramChip').textContent = s.gpuErr || '';
  }
}

function renderGpus(s) {
  const box = $('gpuCards');
  box.innerHTML = '';
  (s.gpus || []).forEach(gp => {
    const used = gp.total - gp.free;
    const d = document.createElement('div');
    d.className = 'gpu';
    d.innerHTML = '<div class="gname">GPU ' + gp.index + ' · ' + esc(gp.name) + '</div>' +
      '<div class="gbar"><div class="gfill" style="width:' + Math.min(100, Math.round(used * 100 / gp.total)) + '%"></div></div>' +
      '<div class="gnum">' + miB(gp.free) + ' / ' + miB(gp.total) + ' ' + tr('free', 'vram_free') + '</div>';
    box.appendChild(d);
  });
  if (!box.children.length && s.gpuErr) box.innerHTML = '<div class="gpu"><div class="gnum">' + esc(s.gpuErr) + '</div></div>';
  renderHost(s);
}

function cpuTitle(n) {
  return String(n || '')
    .replace(/\((?:r|c|tm)\)/gi, '')
    .replace(/\b(cpu|processor)\b/gi, '')
    .replace(/\d+(?:\.\d+)?\s*(?:ghz|mhz)/gi, '')
    .replace(/\s*@\s*/g, ' ')
    .replace(/\s{2,}/g, ' ')
    .trim();
}

function renderHost(s) {
  const box = $('hostCards');
  if (!box) return;
  box.innerHTML = '';
  const h = s.host || {};
  const mk = (title, pct, num) => {
    const d = document.createElement('div');
    d.className = 'gpu';
    d.innerHTML = '<div class="gname">' + esc(title) + '</div>' +
      '<div class="gbar"><div class="gfill" style="width:' + Math.max(0, Math.min(100, pct)) + '%"></div></div>' +
      '<div class="gnum">' + esc(num) + '</div>';
    box.appendChild(d);
  };
  if (h.cpuOK) mk(cpuTitle(h.cpuName) + ' · ' + h.cpuCores + ' ' + t('host_cores'), h.cpuLoad, h.cpuLoad + '% ' + t('host_load'));
  if (h.memOK && h.memTotal) {
    const pct = Math.round(h.memUsed * 100 / h.memTotal);
    mk(t('host_mem'), pct, (h.memUsed / 1073741824).toFixed(1) + ' / ' + (h.memTotal / 1073741824).toFixed(0) + ' GiB · ' + pct + '% ' + t('host_used'));
  }
  if (!box.children.length) box.innerHTML = '<div class="gpu"><div class="gnum">' + esc(h.err || t('host_readfail')) + '</div></div>';
}

function renderMounted(s) {
  const tb = $('mountedRows');
  tb.innerHTML = '';
  const hideSmall = !!s.hideSmall;
  const locals = (s.mounted || []).filter(m => m.host === 'local');
  const shown = hideSmall ? locals.filter(m => m.kindFile !== 'embed' && m.kindFile !== 'vision') : locals;
  shown.forEach(m => {
    const tr_ = document.createElement('tr');
    tr_.setAttribute('data-id', m.id); // 自测用它判「第二行是不是模型自己的名字」
    if (m.running) tr_.className = 'running';
    if (m.fit === 'novram') tr_.className = 'novram';
  let kind = '<span class="badge kind">' + esc(t('kind_' + (m.kind || 'text'))) + '</span>';
    if (m.tools) kind += ' <span class="badge tool">' + esc(t('tag_tools')) + '</span>';
    if (m.reasoner) kind += ' <span class="badge tool">' + esc(t('tag_reason')) + '</span>';
    const need = (m.actKey && m.need) ? '<span class="num">' + miB(m.need) + '</span>' +
      (m.measured ? '<span class="badge measured">' + esc(t('measured')) + '</span>' : '') : '<span class="note">-</span>';
    let state = '';
    if (m.running) state = '<span class="badge running">' + esc(t('st_running')) + '</span>';
    else if (m.fit) state = '<span class="badge ' + esc(m.fit) + '">' + esc(t('st_' + m.fit)) + '</span>';
    else state = '<span class="note">' + esc(m.host === 'local' ? t('up_no') : t('remote')) + '</span>';
    const btn = m.actKey
      ? (m.running
          ? '<button class="btn danger act-stop" data-key="' + esc(m.actKey) + '">' + esc(t('act_stop')) + '</button>'
          : '<button class="btn primary act-start" data-key="' + esc(m.actKey) + '"' + (m.fit === 'novram' ? ' disabled' : '') + '>' + esc(t('act_start')) + '</button>')
      : '<span class="note">-</span>';
    tr_.innerHTML =
      '<td><div class="mname">' + esc(m.name) + '</div><div class="mkey">' + esc(m.ggufName || m.id) + '</div></td>' +
      '<td>' + kind + '</td>' +
      '<td class="mkey">' + esc(m.provider) + (m.port ? ' <span class="note">:' + m.port + '</span>' : '') + '</td>' +
      '<td>' + need + '</td>' +
      '<td class="num">' + (m.port || '') + '</td>' +
      '<td>' + state + '</td>' +
      '<td>' + btn + '</td>';
    tb.appendChild(tr_);
  });
  tb.querySelectorAll('.act-start').forEach(b => b.onclick = () => askStart(b.getAttribute('data-key')));
  tb.querySelectorAll('.act-stop').forEach(b => b.onclick = () => askStop(b.getAttribute('data-key')));
  $('modelsEmpty').classList.toggle('hidden', shown.length > 0);
  if (!shown.length) $('modelsEmpty').textContent = t('mounted_empty') + (s.kindNote ? ' - ' + noteText(s.kindNote) : '');
  const hiddenN = locals.length - shown.length;
  $('modelsHidden').classList.toggle('hidden', hiddenN <= 0);
  if (hiddenN > 0) $('modelsHidden').textContent = t('models_hidden').split('{n}').join(hiddenN);
  $('ckHideSmall').checked = hideSmall;
  if ($('selGPU')) {
    const sel = $('selGPU');
    const want = ['auto'].concat((s.gpus || []).map(g => String(g.index))).join(',');
    const have = Array.from(sel.options).map(o => o.value).join(',');
    if (want !== have) {
      sel.innerHTML = '<option value="auto">' + t('gpu_auto') + '</option>' +
        (s.gpus || []).map(g => '<option value="' + g.index + '">' + g.index + ' · ' + g.name + '</option>').join('');
    }
    sel.value = (s.gpu || 'auto');
  }
}

let LAST_MS = 0;
function stampRefresh(ms) {
  LAST_MS = ms;
  renderRefreshAt();
  try { goApp().Debug('refresh ' + ms + ' ms'); } catch (e) { /* 桥没好时不拦刷新 */ } // 写进日志：以后「刷新慢」能直接看是哪一轮慢
}

function renderRefreshAt() {
  const e = $('refreshAt');
  if (e && LAST_MS) e.textContent = t('refresh_at') + ' ' + new Date().toTimeString().slice(0, 8) + ' (' + LAST_MS + ' ms)';
}

function renderServices(s) {
  const tb = $('serviceRows');
  tb.innerHTML = '';
  (s.services || []).forEach(v => {
    const tr_ = document.createElement('tr');
    if (v.up) tr_.className = 'running';
    const up = v.up ? '<span class="badge running">' + esc(t('up_yes')) + '</span>' : '<span class="badge down">' + esc(t('up_no')) + '</span>';
    let inst = '';
    if (v.installed === true) inst = t('inst_yes');
    else if (v.installed === false) inst = '<span class="badge novram">' + esc(t('inst_no')) + '</span>';
    else inst = '-';
    const w = v.where || {};
    const wh = 'where_' + String(w.how || 'unknown').replace(/^where-/, '').replace(/-/g, '_');
    const loc = w.path ? '<span class="note">' + esc(t(wh)) + '</span><div class="loc">' + esc(w.path) + '</div>'
      : '<span class="note">' + esc(t(wh)) + '</span>';
    const c = v.ctl || {};
    const dk = esc(v.kind) + '|' + (v.port || 0);
    const mk = (act, cls, on) => '<button class="btn ' + cls + ' mini svc-act" data-act="' + act + '" data-dk="' + dk + '"' + (on ? '' : ' disabled') + '>' + esc(t('act_' + act)) + '</button>';
    const ctl = '<div class="svcbtns">' + mk('start', 'primary', c.start) + mk('stop', 'danger', c.stop) +
      mk('rest', 'ghost', c.rest) + mk('force', 'ghost', c.force) + '</div>' +
      (c.start || c.stop ? '' : '<div class="loc">' + esc(v.kind === 'api' ? t('ctl_none_api') : t('ctl_none')) + '</div>');
    tr_.innerHTML =
      '<td><div class="mname">' + esc(svcName(v.name)) + '</div>' + (v.url ? '<div class="mkey">' + esc(v.url) + '</div>' : '') + '</td>' +
      '<td><span class="badge engine">' + esc(t('role_' + v.kind)) + '</span></td>' +
      '<td class="num">' + (v.port || '') + '</td>' +
      '<td>' + up + '</td>' +
      '<td>' + inst + '</td>' +
      '<td class="note">' + esc(noteText(v.note)) + '</td>' +
      '<td>' + loc + '</td>' +
      '<td>' + ctl + '</td>';
    tb.appendChild(tr_);
  });
  tb.querySelectorAll('.svc-act').forEach(b => b.onclick = () => askServiceAct(b.getAttribute('data-dk'), b.getAttribute('data-act')));
  if (!(s.services || []).length) tb.innerHTML = '<tr><td colspan="8" class="empty">' + esc(t('svc_empty')) + '</td></tr>';
}

function svcName(n) {
  if (n && n.indexOf('provider:') === 0) return t('svc_prefix') + ' ' + n.slice(9);
  return n || '';
}

function noteText(n) {
  if (!n) return '';
  const m = /^note-mounted:(\d+)$/.exec(n);
  if (m) return t('note_mounted').replace('{n}', m[1]);
  if (n === 'note-remote') return t('note_remote');
  if (n === 'note-not-installed') return t('note_not_installed');
  if (n === 'note-cache-unreadable') return t('note_cache_unreadable').replace('{url}', (SNAP && SNAP.paths && SNAP.paths.RouterURL) || '');
  if (n === 'note-cache-shape') return t('note_cache_shape');
  if (n === 'note-su-kind') return t('note_su_kind');
  if (n === 'note-ver-dev') return t('note_ver_dev');
  if (n === 'note-kind') return t('note_kind');
  return n;
}

function whereText(w) {
  if (!w) return '';
  if (w.indexOf('where-') === 0) { const k = 'w_' + w.slice(6).replace(/-/g, '_'); const v = t(k); return v === k ? w.slice(6) : v; }
  return w;
}
function schedText(s) {
  if (!s) return '';
  const m = /^every (\d+)s$/.exec(s);
  if (m) {
    const n = parseInt(m[1], 10);
    if (n % 3600 === 0) return tr('every ' + (n / 3600) + ' h', 'sched_h').replace('{n}', n / 3600);
    if (n % 60 === 0) return tr('every ' + (n / 60) + ' min', 'sched_min').replace('{n}', n / 60);
    return tr('every ' + n + ' s', 'sched_s').replace('{n}', n);
  }
  return s;
}

let CRONS = [];
function renderCrons(s) {
  CRONS = s.crons || [];
  const tb = $('cronRows');
  tb.innerHTML = '';
  CRONS.forEach(c => {
    const tr_ = document.createElement('tr');
    const next = c.nextRun ? new Date(c.nextRun).toLocaleString() : '-';
    const stKey = c.lastStatus === 'ok' ? 'ok' : c.lastStatus === 'skipped' ? 'skipped' : c.lastStatus === 'error' ? 'error' : 'none';
    const stTxt =
      stKey === 'ok' ? 'OK' :
      stKey === 'skipped' ? t('cron_st_skipped') :
      stKey === 'error' ? t('cron_st_error') : t('cron_st_none');
    const last = esc(c.lastRun ? new Date(c.lastRun).toLocaleString() : '-') + ' <span class="stl ' + stKey + '">' + esc(stTxt) + '</span>';
    let mTxt = t('cron_script'), mCls = 'note';
    if (c.needs) {
      mCls = 'mkey';
      if (c.runModel) { mTxt = c.runModel + ' · ' + t('cron_run_real'); }
      else if (c.model) { mTxt = c.model + ' · ' + t('cron_pinned'); }
      else { mTxt = t('cron_default'); }
    }
    const modelCell = '<span class="' + mCls + '">' + esc(mTxt) + '</span>';
    const schedCell = c.launchedBy
      ? '<span class="badge switch">' + esc(t('cron_launched').replace('{name}', c.launchedBy)) + '</span>'
      : esc(schedText(c.schedule));
    tr_.innerHTML =
      '<td><div class="mname">' + esc(c.name) + '</div></td>' +
      '<td><span class="st ' + (c.enabled ? 'on' : 'off') + '">' + esc(c.enabled ? t('cron_st_on') : t('cron_st_off')) + '</span></td>' +
      '<td>' + modelCell + '</td>' +
      '<td class="mkey">' + schedCell + '</td>' +
      '<td class="mkey">' + esc(next) + '</td>' +
      '<td class="mkey">' + last + '</td>';
    tb.appendChild(tr_);
  });
  if (!CRONS.length) tb.innerHTML = '<tr><td colspan="6" class="empty">' + esc(t('cron_empty')) + '</td></tr>';
  $('hbState').textContent = [
    t('hb_now') + ': ' + (s.hbModel || '-'),
    t('cp_now') + ': ' + (s.cpModel || '-'),
    t('def_now') + ': ' + (s.defModel || '-'),
  ].join('  ·  ');
}

function opHead(r) {
  if (!r || typeof r === 'string') return String(r || '');
  let s = t('op_' + r.token);
  const miss = Object.keys(r.args || {}).length && (s.match(/\{[a-z_]+\}/g) || []);
  if (miss.length) { // 数据不全：改用只说结论的那句，不留半截话
    const alt = t('op_' + r.token + '_bare');
    s = alt === 'op_' + r.token + '_bare' ? s.replace(/\{[a-z_]+\}/g, '') : alt;
    Object.keys(r.args || {}).forEach(k => { s = s.split('{' + k + '}').join(r.args[k]); });
  } else {
    Object.keys(r.args || {}).forEach(k => { s = s.split('{' + k + '}').join(r.args[k]); });
  }
  if (r.token === 'restart_big_dry' && r.args.ports === 'none') s = s.split('{ports}').join(t('op_ports_none'));
  return s;
}

function opText(r) {
  if (!r) return '';
  if (typeof r === 'string') return r; // 老返回（纯文本）兼容
  const lines = [opHead(r)];
  if (r.rows && r.rows.length) r.rows.forEach(x => lines.push('  ' + x.file + ': ' + x.first + ' / ' + x.later));
  if (r.bad && r.bad.length) lines.push(r.bad.join(' | '));
  if (r.note) lines.push(r.note);
  return lines.join('\n');
}

async function askReserveSet() {
  const f = parseInt($('reserveFirst').value, 10), l = parseInt($('reserveLater').value, 10);
  if (!f || f <= 0) { feed(t('err_prefix') + t('reserve_need')); return; }
  let dry = '', res = null;
  try { dry = opText(await goApp().SetReserve(f, l, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_reserve'), t('reserve_apply') + ': ' + f + ' / ' + l + ' MiB', dry, async () => {
    try { res = await goApp().SetReserve(f, l, false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function askReserveRollback() {
  let dry = '', res = null;
  try { dry = opText(await goApp().RollbackReserve(true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_reserve_rb'), '-', dry, async () => {
    try { res = await goApp().RollbackReserve(false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function askRestartBig() {
  let dry = '', res = null;
  try { dry = opText(await goApp().RestartBigModel(true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_restart_big'), t('restart_big_warn'), dry, async () => {
    try { res = await goApp().RestartBigModel(false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function askServiceAct(dk, act) {
  const parts = String(dk).split('|');
  const kind = parts[0], port = parseInt(parts[1], 10) || 0;
  let dry = '', res = null;
  try { dry = opText(await goApp().ServiceAct(kind, port, act, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_svc'), t('svc_act_warn'), dry, async () => {
    try { res = await goApp().ServiceAct(kind, port, act, false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}
async function askStopEverything() {
  let dry = '', res = null;
  try { dry = opText(await goApp().StopEverything(true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('btn_stop_everything'), t('stop_everything_hint'), dry, async () => {
    try { res = await goApp().StopEverything(false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}
async function askGwRescue(hard) {
  let dry = '', res = null;
  try { dry = opText(await goApp().RescueGateway(hard, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_rescue') + (hard ? ' (' + t('rescue_hard') + ')' : ''), t('rescue_hint'), dry, async () => {
    try { res = await goApp().RescueGateway(hard, false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function askClearCache() {
  let dry = '', res = null;
  try { dry = opText(await goApp().ClearBigCache(true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_cache'), t('cache_warn'), dry, async () => {
    try { res = await goApp().ClearBigCache(false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

function renderVersions(s) {
  const tb = $('versionRows');
  tb.innerHTML = '';
  (s.versions || []).forEach(v => {
    const tr_ = document.createElement('tr');
    const verdict = v.update === 'up-to-date' ? '<span class="badge measured">' + esc(t('ver_ok')) + '</span>'
      : v.update === 'behind' ? '<span class="badge novram">' + esc(t('ver_behind')) + '</span>'
      : v.update === 'ahead' ? '<span class="badge measured">' + esc(t('ver_ahead')) + '</span>'
: '<span class="note">' + esc(v.note || t('ver_unknown')) + '</span>';
    tr_.innerHTML =
      '<td><div class="mname">' + esc(v.what === 'forgeboard' ? t('ver_self_name') : v.what) + '</div></td>' +
      '<td class="num">' + esc(v.local || '-') + '</td>' +
      '<td class="num">' + esc(v.latest || '-') + '</td>' +
      '<td class="mkey">' + esc(v.date || '') + '</td>' +
      '<td>' + verdict + '</td>' +
      '<td class="mkey">' + (v.upstream ? '<a href="' + esc(v.upstream) + '" target="_blank" rel="noreferrer">' + esc(t('ver_link')) + '</a>' : '') + '</td>';
    tb.appendChild(tr_);
  });
  if (!(s.versions || []).length) tb.innerHTML = '<tr><td colspan="6" class="empty">' + esc(t('ver_empty')) + '</td></tr>';
}

function renderPowers(s) {
  const tb = $('powerRows');
  tb.innerHTML = '';
  (s.powers || []).forEach(c => {
    const tr_ = document.createElement('tr');
    if (!c.supported) {
      tr_.innerHTML = '<td><div class="mname">' + esc(c.name) + '</div></td><td colspan="4" class="note">' + esc(c.note || t('pw_none')) + '</td><td></td>';
      tb.appendChild(tr_);
      return;
    }
    tr_.innerHTML =
      '<td><div class="mname">GPU ' + c.index + ' · ' + esc(c.name) + '</div></td>' +
      '<td class="num">' + c.current + ' W</td>' +
      '<td class="num">' + c.default + ' W</td>' +
      '<td class="num">' + c.min + '-' + c.max + ' W</td>' +
      '<td><input class="watts" type="number" min="' + c.min + '" max="' + c.max + '" step="5" value="' + c.current + '" data-i="' + c.index + '"></td>' +
      '<td><button class="btn ghost act-power" data-i="' + c.index + '">' + esc(t('pw_apply')) + '</button></td>';
    tb.appendChild(tr_);
  });
  if (!(s.powers || []).length) tb.innerHTML = '<tr><td colspan="6" class="empty">' + esc(t('pw_empty')) + '</td></tr>';
  tb.querySelectorAll('.act-power').forEach(b => b.onclick = () => {
    const inp = tb.querySelector('input.watts[data-i="' + b.getAttribute('data-i') + '"]');
    askPower(parseInt(b.getAttribute('data-i'), 10), parseInt(inp.value, 10));
  });
}

async function askPower(index, watts) {
  if (!watts || watts <= 0) { feed(t('err_prefix') + t('pw_need_watts')); return; }
  let dry = '';
  try { dry = await goApp().SetPowerWatts(index, watts, true); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_pw'), 'GPU ' + index + ' -> ' + watts + ' W', dry, async () => {
    await applyPower(index, watts);
  });
}

async function applyPower(index, watts) {
  try {
    let r = await goApp().SetPowerWatts(index, watts, false);
    if (String(r).indexOf('need-elevation') === 0) {
      closeConfirm();
      openConfirm(t('confirm_title_pw'), t('pw_elevate_q'), '', async () => {
        try { feed(await goApp().SetPowerElevated(index, watts)); }
        catch (e) { feed(t('err_prefix') + (e.message || e)); }
        await refresh();
      });
      $('confirmYes').textContent = t('pw_elevate_yes');
      $('confirmNo').textContent = t('pw_elevate_no_btn');
      return;
    }
    feed(String(r));
  } catch (e) { feed(t('err_prefix') + (e.message || e)); }
  await refresh();
}

function renderReserve(s) {
  const tb = $('reserveRows');
  tb.innerHTML = '';
  const rows = s.reserve || [];
  rows.forEach(r => {
    const tr_ = document.createElement('tr');
    tr_.innerHTML = '<td class="mkey">' + esc(r.file) + '</td><td class="num">' + r.first + ' MiB</td><td class="num">' + r.later + ' MiB</td>';
    tb.appendChild(tr_);
  });
  if (!rows.length) tb.innerHTML = '<tr><td colspan="3" class="empty">' + esc(t('reserve_empty')) + '</td></tr>';
  if (rows.length && !$('reserveFirst').value) {
    $('reserveFirst').value = rows[0].first;
    $('reserveLater').value = rows[0].later;
  }
  const c = s.cache || {};
  $('cacheState').textContent = c.readable
    ? t('cache_now') + ': ' + c.parked + '/' + c.slots + ' · ' + (c.bytes / 1073741824).toFixed(2) + ' GiB'
    : (noteText(c.note) || t('cache_none'));
}

const SU_SRC = {
  'hkcu-run': 'su_src_hkcu', 'hklm-run': 'su_src_hklm', 'hklm-run32': 'su_src_hklm32',
  'folder-user': 'su_src_folder_user', 'folder-common': 'su_src_folder_common',
};

function fmtTok(n) {
  n = Number(n) || 0;
  if (LANG === 'zh') {
    if (n >= 1e8) return (n / 1e8).toFixed(4) + ' 亿';
    if (n >= 1e4) return (n / 1e4).toFixed(2) + ' 万';
    return String(n);
  }
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B';
  if (n >= 1e6) return (n / 1e6).toFixed(1) + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'k';
  return String(n);
}
const US_RATE_FALLBACK = 7.1;
function fmtCost(c, rate) {
  c = Number(c) || 0;
  if (c === 0) return '-';
  if (LANG === 'zh') {
    const r = Number(rate) > 0 ? Number(rate) : US_RATE_FALLBACK;
    const v = c * r;
    return '\u00a5' + (v < 1 ? v.toFixed(3) : v.toFixed(2));
  }
  return '$' + (c < 1 ? c.toFixed(3) : c.toFixed(2));
}
function usAge(sec) {
  sec = Number(sec) || 0;
  if (sec < 90) return t('us_age_sec').split('{n}').join(sec);
  if (sec < 5400) return t('us_age_min').split('{n}').join(Math.round(sec / 60));
  if (sec < 172800) return t('us_age_hour').split('{n}').join(Math.round(sec / 3600));
  return t('us_age_day').split('{n}').join(Math.round(sec / 86400));
}
function usToday() {
  const d = new Date();
  const p = (x) => String(x).padStart(2, '0');
  return d.getFullYear() + '-' + p(d.getMonth() + 1) + '-' + p(d.getDate());
}
function usStat(k, v, sub, paid) {
  return '<div class="us-stat' + (paid ? ' paid' : '') + '"><div class="us-k">' + esc(k) + '</div>' +
    '<div class="us-v">' + esc(v) + '</div><div class="us-s">' + esc(sub) + '</div></div>';
}
function usCostCell(m, rate) {
  if (m.local) return '<td class="us-num us-free">' + esc(t('us_local_free')) + '</td>';
  if (m.paid) return '<td class="us-num">' + esc(fmtCost(m.cost, rate)) + '</td>';
  return '<td class="us-num">' + esc(t('us_free')) + '</td>';
}
function usNote(note) {
  return t('us_note_' + String(note).replace(/^usage_/, ''));
}
function renderUsage(s) {
  const u = s.usage || {};
  const chip = $('tokChip');
  const st = $('usState');
  if (!st) return;
  const today = u.today || { tokens: 0 };
  const d7 = u.d7 || { tokens: 0 };
  const tot = u.totals || { tokens: 0 };
  if (chip) chip.textContent = t('us_chip').split('{n}').join(fmtTok(today.tokens));
  if (u.note) st.textContent = usNote(u.note);
  else if (!u.at) st.textContent = t('us_state_never');
  else st.textContent = t('us_state')
    .split('{at}').join(new Date(u.at).toLocaleTimeString())
    .split('{age}').join(usAge(u.ageSec))
    .split('{windows}').join(u.windows);
  $('usStats').innerHTML = [
    usStat(t('us_today'), fmtTok(today.tokens), fmtCost(today.cost, u.rate)),
    usStat(t('us_7d'), fmtTok(d7.tokens), fmtCost(d7.cost, u.rate)),
    usStat(t('us_30d'), fmtTok(tot.tokens), fmtCost(tot.cost, u.rate)),
  ].join('');
  const models = u.models || [];
  const maxTok = models.reduce((m, x) => Math.max(m, x.d30 || 0), 0);
  $('usModelRows').innerHTML = models.map(m => {
    const name = (m.provider ? m.provider + '/' : '') + (m.model || '');
    const w = maxTok > 0 && m.d30 ? Math.max(2, Math.round((m.d30 / maxTok) * 100)) : 0;
    return '<tr' + (m.today ? '' : ' class="us-zero"') + '><td><div class="mname">' + esc(name) + '</div>' +
      (w ? '<div class="us-bar"><i style="width:' + w + '%"></i></div>' : '') + '</td>' +
      '<td class="us-tok">' + (m.today ? fmtTok(m.today) : '-') + '</td>' +
      '<td class="us-num">' + fmtTok(m.d7) + '</td><td class="us-num">' + fmtTok(m.d30) + '</td>' +
      usCostCell(m, u.rate) + '</tr>';
  }).join('') || '<tr><td colspan="5" class="empty">' + esc(t('us_empty')) + '</td></tr>';
  const un = $('usUnpriced');
  if (un) un.textContent = tot.unpriced ? t('us_unpriced').split('{n}').join(tot.unpriced) : '';
  const rt = $('usRate');
  if (rt) {
    const rate = Number(u.rate) || 0;
    rt.textContent = (LANG === 'zh' && tot.cost)
      ? t(rate > 0 ? 'us_rate_note' : 'us_rate_fallback').split('{rate}').join(rate > 0 ? rate.toFixed(2) : US_RATE_FALLBACK)
      : '';
  }
}

function renderStartup(s) {
  const tb = $('startupRows');
  const rows = s.startup || [];
  tb.innerHTML = '';
  rows.forEach(r => {
    const tr_ = document.createElement('tr');
    const st = '<span class="stl ' + (r.enabled ? 'ok' : 'error') + '">' + esc(r.enabled ? t('su_state_on') : t('su_state_off')) + '</span>';
    tr_.innerHTML =
      '<td><div class="mname">' + esc(r.name) + '</div></td>' +
      '<td class="mkey">' + esc(t(SU_SRC[r.source] || 'su_src_hkcu')) + '</td>' +
      '<td class="mkey">' + esc(noteText(r.note) || r.command) + '</td>' +
      '<td>' + st + '</td>';
    tb.appendChild(tr_);
  });
  if (!rows.length) tb.innerHTML = '<tr><td colspan="4" class="empty">' + esc(t('su_empty')) + '</td></tr>';
  const on = !!s.selfOn;
  $('selfState').textContent = (on ? t('self_on') : t('self_off')) + (s.selfEntry ? ' · ' + s.selfEntry : '');
  $('btnSelfOn').disabled = on;
  $('btnSelfOff').disabled = !on;
  $('btnOpenFolder').disabled = !s.selfDir;
  $('btnOpenFolder').title = s.selfDir || '';
}

const TK_TRIG = {
  BootTrigger: 'tk_trig_boot', LogonTrigger: 'tk_trig_logon', SessionStateChangeTrigger: 'tk_trig_session',
  TimeTrigger: 'tk_trig_time', CalendarTrigger: 'tk_trig_calendar', DailyTrigger: 'tk_trig_daily',
  EventTrigger: 'tk_trig_event', RegistrationTrigger: 'tk_trig_reg', IdleSettingsTrigger: 'tk_trig_idle',
  MaintenanceTrigger: 'tk_trig_maint', PluginTrigger: 'tk_trig_plugin', RepetitionTrigger: 'tk_trig_repeat',
  SysmodTrigger: 'tk_trig_sysmod', StateChangeTrigger: 'tk_trig_state', Trigger: 'tk_trig_other',
};
const TK_STATE = { running: 'tk_st_running', ready: 'tk_st_ready', queued: 'tk_st_queued', disabled: 'tk_st_disabled' };
const TK_RESULT = { ok: 'tk_res_ok', running: 'tk_res_running', notrun: 'tk_res_notrun', noschedule: 'tk_res_noschedule', manual: 'tk_res_manual', recurrence: 'tk_res_recurrence', failed: 'tk_res_failed', unknown: 'tk_res_unknown' };

function renderTasks(s) {
  const tb = $('taskRows');
  const all = s.tasks || [];
  const hideSys = $('ckTkSys') ? $('ckTkSys').checked : true;
  const rows = hideSys ? all.filter(r => !r.system) : all;
  tb.innerHTML = '';
  rows.forEach(r => {
    const tr_ = document.createElement('tr');
    if (r.state === 'running') tr_.className = 'running';
    const where = r.system ? t('tk_src_system') : t('tk_src_user');
    const flags = r.hidden ? '<span class="badge switch">' + esc(t('tk_hidden')) + '</span>' : '';
    const trig = String(r.trigger || '').split(',').map(x => x.trim()).filter(Boolean)
      .map(x => { const k = TK_TRIG[x]; return k ? t(k) : x; }).join(' + ') || '-';
    const stKey = TK_STATE[r.state] ? r.state : 'unknown';
    const stCls = r.state === 'running' ? 'on' : r.state === 'disabled' ? 'dis' : 'ready';
    const st = '<span class="st ' + stCls + '">' + esc(t(TK_STATE[stKey] || 'tk_st_unknown')) + '</span>';
    const resKey = TK_RESULT[r.result] ? r.result : 'unknown';
    const resCls = resKey === 'ok' ? 'ok' : resKey === 'failed' ? 'error' : resKey === 'running' ? 'skipped' : 'none';
    const res = '<span class="stl ' + resCls + '">' + esc(t(TK_RESULT[resKey]) + (r.resultNote ? ' ' + r.resultNote : '')) + '</span>';
    tr_.innerHTML =
      '<td><div class="mname">' + esc(r.name) + '</div></td>' +
      '<td class="mkey">' + esc(where) + (flags ? ' ' + flags : '') + '</td>' +
      '<td class="mkey">' + trig + '</td>' +
      '<td>' + st + '</td>' +
      '<td class="mkey">' + esc(r.lastRun ? new Date(r.lastRun).toLocaleString() : '-') + '</td>' +
      '<td>' + res + '</td>';
    tb.appendChild(tr_);
  });
  if (!rows.length) {
    const msg = s.taskNote === 'tasks-loading' ? t('tk_loading') : t('tk_empty') + (s.taskNote ? ' - ' + noteText(s.taskNote) : '');
    tb.innerHTML = '<tr><td colspan="6" class="empty">' + esc(msg) + '</td></tr>';
  }
  const cnt = (hideSys && rows.length !== all.length)
    ? t('tk_count_user').split('{n}').join(rows.length).split('{total}').join(all.length)
    : t('tk_count').split('{n}').join(rows.length);
  $('tkState').textContent = cnt + ' · ' + t('tk_note_short');
}

async function askSuSwitch(source, name, enable) {
  let dry = '';
  try { dry = opText(await goApp().StartupSwitchItem(source, name, enable, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_su'), name + ' -> ' + (enable ? t('su_state_on') : t('su_state_off')), dry, async () => {
    try { feed(opText(await goApp().StartupSwitchItem(source, name, enable, false))); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function askSuDelete(source, name) {
  let dry = '';
  try { dry = opText(await goApp().StartupDeleteItem(source, name, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_su_del'), name, dry, async () => {
    try { feed(opText(await goApp().StartupDeleteItem(source, name, false))); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function askSelfStart(on) {
  let dry = '';
  try { dry = opText(await goApp().SetSelfStart(on, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_self'), on ? t('btn_self_on') : t('btn_self_off'), dry, async () => {
    try { feed(opText(await goApp().SetSelfStart(on, false))); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

function renderSystem(s) {
  const p = s.paths || {};
  $('pMODELS').value = p.MODELS || '';
  $('pLLAMA_EXE').value = p.LLAMA_EXE || '';
  $('pTIER_DIR').value = p.TIER_DIR || '';
  $('pRouterURL').value = p.RouterURL || '';
  $('pSwapPort').value = p.SwapPort || '';
  $('pCandidatePorts').value = (p.CandidatePorts || []).join(', ');
  $('pOC_HOME').value = p.OC_HOME || '';
  $('pOC_CFG').value = p.OC_CFG || '';
  $('pGW_RESCUE').value = p.GW_RESCUE || '';
  const banner = $('ocBanner');
  if (!s.ocReady) { banner.classList.remove('hidden'); banner.textContent = t('oc_missing') + (s.ocNote || ''); }
  else banner.classList.add('hidden');
  renderSessions(s);
  renderReserve(s);
}

let picked = new Set();

function renderSessions(s) {
  const sel = $('targetModel');
  const keep = sel.value;
  sel.innerHTML = '';
  (s.targets || []).forEach(m => {
    const o = document.createElement('option');
    o.value = m.ref;
    o.textContent = m.ref + (m.cap ? '  (' + Math.round(m.cap / 1024) + 'k ctx)' : '');
    sel.appendChild(o);
  });
  if (keep) sel.value = keep;
  $('ocState').textContent = s.ocReady ? '' : t('oc_missing') + (s.ocNote || '');

  const sr = $('sessionRows');
  sr.innerHTML = '';
  const hideSys = $('ckHideSys').checked;
  const ws = (s.windows || []).filter(w => !hideSys || w.kind === 'chat');
  const keys = new Set(ws.map(w => w.key));
  for (const k of [...picked]) if (!keys.has(k)) picked.delete(k);
  ws.forEach(w => {
    const tr_ = document.createElement('tr');
    if (w.running) tr_.className = 'running';
    const used = w.cap ? (Math.round(w.used * 100 / w.cap) + '% · ' + Math.round(w.used / 1024) + 'k / ' + Math.round(w.cap / 1024) + 'k') : String(w.used);
    const kind = w.kind === 'chat' ? '' : '<span class="badge">' + esc(tr('job', 'badge_job')) + '</span>';
    tr_.innerHTML =
      '<td class="ck"><input type="checkbox" data-key="' + esc(w.key) + '"' + (picked.has(w.key) ? ' checked' : '') + '></td>' +
      '<td><div class="mname">' + esc(w.name) + '</div><div class="mkey">' + esc(w.key) + '</div></td>' +
      '<td>' + esc(whereText(w.where)) + ' ' + kind + '</td>' +
      '<td class="mkey">' + esc(w.model) + '</td>' +
      '<td class="num">' + esc(used) + '</td>' +
      '<td>' + (w.running ? '<span class="badge running">' + esc(tr('answering', 'st_answering')) + '</span>' : '') + '</td>';
    sr.appendChild(tr_);
  });
  if (!ws.length) sr.innerHTML = '<tr><td colspan="6" class="empty">' + esc(s.ocNote || t('sessions_empty')) + '</td></tr>';
  $('ckHideSys').onchange = () => { if (SNAP) renderSessions(SNAP); };
  sr.querySelectorAll('input[type=checkbox]').forEach(cb => {
    cb.onchange = () => { const k = cb.getAttribute('data-key'); if (cb.checked) picked.add(k); else picked.delete(k); };
  });
  $('ckAll').onchange = (e) => {
    picked = new Set();
    sr.querySelectorAll('input[type=checkbox]').forEach(cb => {
      cb.checked = e.target.checked;
      if (e.target.checked) picked.add(cb.getAttribute('data-key'));
    });
  };
}

async function switchSelected() {
  const model = $('targetModel').value;
  const keys = [...picked];
  if (!model) { feed(t('err_prefix') + t('need_target')); return; }
  if (!keys.length) { feed(t('err_prefix') + t('need_pick')); return; }
  let dry = '';
  try { dry = await goApp().SwitchWindows(model, keys, true); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_switch'), keys.length + ' ' + t('windows_n'), dry, async () => {
    try { feed(await goApp().SwitchWindows(model, keys, false)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

function selectChatWindows() {
  picked = new Set();
  const sr = $('sessionRows');
  sr.querySelectorAll('input[type=checkbox]').forEach(cb => cb.checked = false);
  (SNAP && SNAP.windows || []).forEach(w => {
    if (w.kind !== 'chat') return;
    picked.add(w.key);
    const cb = sr.querySelector('input[data-key="' + w.key.replace(/"/g, '\\"') + '"]');
    if (cb) cb.checked = true;
  });
}

function render(s) {
  SNAP = s;
  if (s.lang && s.lang !== LANG) { LANG = s.lang; $('langSel').value = LANG; } // 语言以配置为准（跑完自检/重启都不跳）
  renderEntry(s);
  renderTop(s); renderGpus(s); renderMounted(s); renderServices(s); renderCrons(s); renderVersions(s); renderUsage(s); renderPowers(s); renderStartup(s); renderTasks(s); renderSystem(s);
}

async function refresh() {
  const t0 = Date.now();
  try {
    render(await goApp().Snapshot());
    stampRefresh(Date.now() - t0);
  } catch (e) { feed(t('err_prefix') + (e.message || e)); }
}

let pending = null;

function openConfirm(title, body, dry, onYes) {
  pending = onYes;
  $('confirmTitle').textContent = title;
  $('confirmBody').textContent = body;
  const d = $('confirmDry');
  if (dry) { d.classList.remove('hidden'); d.textContent = t('confirm_dry_hint') + '\n' + dry; }
  else d.classList.add('hidden');
  $('confirmYes').textContent = t('confirm_yes');
  $('confirmNo').textContent = t('confirm_no');
  $('confirm').classList.remove('hidden');
}
function closeConfirm() { $('confirm').classList.add('hidden'); pending = null; }

function renderEntry(s) {
  const el = $('entryLine');
  if (!el) return;
  const port = s && s.swap && s.swap.port ? s.swap.port : 0;
  el.textContent = t('entry_label') + ' ' + (port ? 'http://127.0.0.1:' + port + '/v1' : t('entry_none'));
}

function findModel(key) { return (SNAP && SNAP.models || []).find(m => m.key === key); }

async function askStart(key) {
  const m = findModel(key);
  if (!m) return;
  let dry = '';
  try { dry = opText(await goApp().StartModel(key, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_start'), m.name + '  ·  ' + m.key + '  ·  ' + miB(m.need), dry, () => runAction(async () => { feed(opText(await goApp().StartModel(key, false))); }));
}

async function askReloadSwap() {
  let dry = '';
  try { dry = opText(await goApp().ReloadSwap(true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_reload'), t('btn_reload_swap'), dry, async () => {
    try { feed(opText(await goApp().ReloadSwap(false))); } catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function doUsageNow() {
  const st = $('usState');
  if (st) st.textContent = t('us_collecting');
  const btn = $('btnUsageNow');
  if (btn) btn.disabled = true;
  try {
    renderUsage({ usage: await goApp().UsageNow() });
  } catch (e) {
    feed(t('err_prefix') + (e.message || e));
  }
  if (btn) btn.disabled = false;
}

async function askStop(key) {
  const m = findModel(key);
  if (!m) return;
  let dry = '';
  try { dry = opText(await goApp().StopModel(key, true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_stop'), m.name + '  ·  ' + m.key, dry, () => runAction(async () => { feed(opText(await goApp().StopModel(key, false))); }));
}

async function runAction(fn) {
  closeConfirm();
  try { await fn(); } catch (e) { feed(t('err_prefix') + (e.message || e)); }
  await refresh();
}

let DETECT = null;
function renderDetect() {
  const tb = $('detectRows');
  if (!tb) return;
  tb.innerHTML = '';
  (DETECT || []).forEach(r => {
    const tr_ = document.createElement('tr');
    const val = r.value ? '<span class="mkey">' + esc(r.value) + '</span>' : '<span class="note">' + esc(t('detect_missing')) + '</span>';
    tr_.innerHTML = '<td class="mkey">' + esc(r.field) + '</td><td>' + val + '</td><td class="mkey">' + esc(r.how) + '</td>';
    tb.appendChild(tr_);
  });
  $('detectTable').classList.toggle('hidden', !(DETECT || []).length);
  const any = (DETECT || []).some(r => r.value);
  const ba = $('btnDetectApply');
  if (ba) ba.classList.toggle('hidden', !any);
}

async function doDetect() {
  try {
    DETECT = await goApp().DetectPaths();
    renderDetect();
    feed(t('detect_box') + ': ' + (DETECT || []).filter(r => r.value).length + ' ' + t('detect_found'));
  } catch (e) { feed(t('err_prefix') + (e.message || e)); }
}

async function doDetectApply() {
  try { feed(await goApp().ApplyDetect()); DETECT = null; renderDetect(); }
  catch (e) { feed(t('err_prefix') + (e.message || e)); }
  await refresh();
}

async function doStopAll() {
  let dry = '', res = null;
  try { dry = opText(await goApp().StopAll(true)); } catch (e) { dry = t('err_prefix') + (e.message || e); }
  openConfirm(t('confirm_title_stopall'), t('stopall_hint'), dry, async () => {
    try { res = await goApp().StopAll(false); feed(opText(res)); }
    catch (e) { feed(t('err_prefix') + (e.message || e)); }
    await refresh();
  });
}

async function doDoctor() {
  try { $('doctorOut').textContent = await goApp().DoctorText(); }
  catch (e) { $('doctorOut').textContent = t('err_prefix') + (e.message || e); }
}

async function doSave() {
  const p = {
    MODELS: $('pMODELS').value.trim(),
    LLAMA_EXE: $('pLLAMA_EXE').value.trim(),
    TIER_DIR: $('pTIER_DIR').value.trim(),
    RouterURL: $('pRouterURL').value.trim(),
    SwapPort: parseInt($('pSwapPort').value.trim(), 10) || 0,
    CandidatePorts: $('pCandidatePorts').value.split(/[,\s]+/).map(x => parseInt(x, 10)).filter(x => x > 0),
    OC_HOME: $('pOC_HOME').value.trim(),
    OC_CFG: $('pOC_CFG').value.trim(),
    GW_RESCUE: $('pGW_RESCUE').value.trim(),
  };
  try { await goApp().SavePaths(p); $('saveMsg').textContent = t('saved'); }
  catch (e) { $('saveMsg').textContent = t('save_fail') + (e.message || e); }
  await refresh();
}

function showTab(name) {
  TAB = name;
  document.querySelectorAll('.tab').forEach(b => b.classList.toggle('active', b.getAttribute('data-tab') === name));
  document.querySelectorAll('.page').forEach(p => p.classList.toggle('active', p.id === 'page-' + name));
}

function mainRight() { const m = document.querySelector('main'); return Math.round(m.getBoundingClientRect().right); }

// 自检换语言时只换界面文字：不写配置、不动后端语言（跑完不会留在英文）。
function setLangQuiet(lang) {
  LANG = lang === 'zh' ? 'zh' : 'en';
  applyI18n();
}

function startAutoRefresh() {
  if (window.FB_TIMER) clearInterval(window.FB_TIMER);
  window.FB_TIMER = setInterval(refresh, 15000); // 状态自己动（模型闲置卸载、别的窗口起停），15 秒看一眼
}
async function selfTestRun() {
  const rep = [];
  const prevTab = TAB;
  showTab('models'); // 不在 Models 页时那一页的按钮全是 0x0，量了会误判成「按钮没了」
  const rect = (id) => { const e = $(id); if (!e) return 'MISSING'; const r = e.getBoundingClientRect(); return Math.round(r.width) + 'x' + Math.round(r.height) + '@' + Math.round(r.left) + ',' + Math.round(r.top); };
  rep.push('tabs=' + document.querySelectorAll('.tab').length + ' pages=' + document.querySelectorAll('.page').length);
  ['btnRefresh', 'btnStopAll', 'btnSave', 'btnDoctor', 'langSel'].forEach(id => rep.push(id + ' ' + rect(id) + ' disp=' + ($(id) ? getComputedStyle($(id)).display : '-')));
  const hd = document.querySelector('header').getBoundingClientRect();
  rep.push('topH=' + Math.round(hd.height) + 'px overflowX=' + (document.querySelector('header').scrollWidth > document.querySelector('header').clientWidth));
  const navR = document.querySelector('nav#tabs').getBoundingClientRect();
  const tops = new Set(Array.from(document.querySelectorAll('#cardRow .gpu')).map((e) => Math.round(e.getBoundingClientRect().top)));
  const widths = Array.from(document.querySelectorAll('#cardRow .gpu')).map((e) => Math.round(e.getBoundingClientRect().width));
  rep.push('topRows=' + document.querySelectorAll('header .hrow').length +
    ' tabsInHeader=' + (navR.top >= hd.top && navR.bottom <= hd.bottom ? 'y' : 'NO') +
    ' tabsRightGap=' + Math.round(hd.right - navR.right) +
    ' chipsRightGap=' + Math.round(hd.right - document.querySelector('.chipsrow').getBoundingClientRect().right) +
    ' cardTops=' + tops.size + ' cardWidths=' + widths.join('/') +
    ' cardNameClipped=' + Array.from(document.querySelectorAll('#cardRow .gpu .gname')).map((e) => (e.scrollWidth > e.clientWidth + 1 ? 'YES' : 'no')).join('/') +
    ' btnRowRightGap=' + Math.round(mainRight() - document.querySelector('#cardRow').nextElementSibling.getBoundingClientRect().right));
  const logoEl = document.querySelector('.logo');
  rep.push('logoImg=' + (logoEl && logoEl.tagName === 'IMG' && logoEl.naturalWidth > 0 ? 'y ' + logoEl.naturalWidth + 'x' + logoEl.naturalHeight : 'NO') +
    ' stopTitleGone=' + (String($('btnStopAll').title || '').length === 0 ? 'y' : 'NO'));
  const noteEl = $('modelNotes');
  const rowbtnsEl = document.querySelector('#cardRow').nextElementSibling.querySelector('.rowbtns');
  rep.push('noteLines=' + (noteEl ? noteEl.children.length : 0) + ' noteLen=' + String(noteEl ? noteEl.textContent : '').length +
    ' noteShown=' + (noteEl && noteEl.offsetHeight > 0 ? 'y' : 'NO') +
    ' rowbtnsRightGap=' + Math.round(mainRight() - rowbtnsEl.getBoundingClientRect().right) +
    ' cacheInModels=' + ($('btnClearCache') && !!$('btnClearCache').closest('#page-models') ? 'y' : 'NO') +
    ' cacheInServices=' + ($('btnClearCache') && !!$('btnClearCache').closest('#page-services') ? 'YES' : 'no') +
    ' reserveInModels=' + ($('reserveBox') && !!$('reserveBox').closest('#page-models') ? 'y' : 'NO') +
    ' reserveInSystem=' + ($('reserveBox') && !!$('reserveBox').closest('#page-system') ? 'YES' : 'no') +
    ' verInServices=' + ($('versionRows') && !!$('versionRows').closest('#page-services') ? 'y' : 'NO') +
    ' verInSystem=' + ($('versionRows') && !!$('versionRows').closest('#page-system') ? 'YES' : 'no'));
  const tg = $('engineState');
  const tierNow = SNAP && SNAP.router && SNAP.router.current ? String(SNAP.router.current) : '';
  const tgTxt = String(tg ? tg.textContent : '');
  rep.push('taglineLen=' + tgTxt.length + ' taglineDupTier=' + (tierNow && tgTxt.indexOf(tierNow) >= 0 ? 'YES' : 'no'));
  rep.push('cardOrder=' + Array.from(document.querySelector('.rowbar').children).map(c => c.id).join(',') +
    ' curModel=' + $('curModel').textContent.slice(0, 44) + ' tierUp=' + (((SNAP && SNAP.router && SNAP.router.tiers) || []).filter(x => x.running).map(x => x.tier + ':' + x.port).join('+') || 'none') + ' monLink=' + $('monLink').textContent.slice(0, 26) +
    ' refreshAt=' + $('refreshAt').textContent.slice(0, 30) +
 ' ocLink=' + $('ocLink').textContent.slice(0, 26) + ' ocTitle=' + String($('ocLink').title || '').slice(0, 30) +
 ' ocWired=' + (typeof $('ocLink').onclick === 'function' ? 'y' : 'no'));
  rep.push('colorScheme=' + getComputedStyle(document.documentElement).colorScheme + ' optionBg=' + (() => { const o = document.querySelector('#langSel option'); return o ? getComputedStyle(o).backgroundColor : 'none'; })());
  const first = $('mountedRows').querySelector('.act-start');
  rep.push('firstStartBtn=' + (first ? (first.getBoundingClientRect().width > 0 ? 'visible' : 'zero-width') : 'none'));
  const runRow = $('mountedRows').querySelector('tr.running');
  rep.push('ver=' + String((SNAP && (SNAP.stamp || SNAP.version)) || 'none') + ' btnCount=' + document.querySelectorAll('.btn').length);
  rep.push('runningRow=' + (runRow ? runRow.textContent.replace(/\s+/g, ' ').trim().slice(0, 70) : 'none'));
  rep.push('mountedRows=' + $('mountedRows').children.length + ' boxesGone=' + ($('remoteBox') || $('unmountedBox') || $('modelRows') ? 'present' : 'y') +
    ' kindBadges=' + $('mountedRows').querySelectorAll('.badge.kind').length +
    ' serviceRows=' + $('serviceRows').children.length + ' cronRows=' + $('cronRows').children.length);
  try {
    const g = await Promise.race([
      goApp().RestartGateway(true).then(v => 'ok:' + String(v).replace(/\s+/g, ' ').slice(0, 50)).catch(e => 'err:' + (e.message || e)),
      new Promise(res => setTimeout(() => res('TIMEOUT-6s'), 6000)),
    ]);
    rep.push('gwDry=' + g);
  } catch (e) { rep.push('gwDry-threw ' + (e.message || e)); }
  rep.push('cronBtnGone=' + ($('btnCronSwitch') ? 'N' : 'y') + ' cronActCells=' + $('cronRows').querySelectorAll('button').length);
  const stAll = $('cronRows').querySelectorAll('span.st');
  const stlAll = $('cronRows').querySelectorAll('span.stl');
  const stOk = $('cronRows').querySelectorAll('span.stl.ok').length;
  const snapOk = ((SNAP && SNAP.crons) || []).filter((c) => c.lastStatus === 'ok').length;
  const stTxtSeen = Array.from(stAll).map((e) => e.textContent).join('/');
  const pill = $('cronRows').querySelector('span.stl.ok');
  const cs = pill ? getComputedStyle(pill) : null;
  const warnEl = document.querySelector('[data-i18n="cron_hb_warn"]');
  const wcs = warnEl ? getComputedStyle(warnEl) : null;
  rep.push('cronStCells=' + stAll.length + '/' + stlAll.length + ' cronStOk=' + stOk + ' snapOk=' + snapOk +
    ' cronStMatch=' + (stOk === snapOk ? 'yes' : 'NO') + ' cronStTxt=' + stTxtSeen.slice(0, 40) +
    ' cronWarnLine=' + (warnEl ? 'y' : 'N') + ' warnBorder=' + (wcs ? wcs.borderColor + '/' + wcs.color : '-') +
    ' pillStyle=' + (cs ? cs.backgroundColor + ' txt=' + cs.color + ' r=' + cs.borderRadius + ' w=' + cs.fontWeight + ' size=' + cs.fontSize + ' h=' + pill.offsetHeight : 'NO-PILL'));
  const rowsBefore = $('mountedRows').children.length;
  if (SNAP) renderMounted(Object.assign({}, SNAP, { hideSmall: !SNAP.hideSmall }));
  const rowsHidden = $('mountedRows').children.length;
  rep.push('hideCk=' + ($('ckHideSmall') ? 'y' : 'N') + ' hideWorks=' + (rowsBefore !== rowsHidden ? 'yes' : 'no') +
    ' rows=' + rowsBefore + '->' + rowsHidden + ' hiddenTxt=' + String($('modelsHidden').textContent || '').slice(0, 34) +
    ' stopBtn=' + String($('btnStopAll').textContent || '').slice(0, 26) + ' stopTitle=' + String($('btnStopAll').title || '').slice(0, 26));
  if (SNAP) renderMounted(SNAP);
  rep.push('versionRows=' + $('versionRows').children.length +
    ' verSelf=' + (Array.from($('versionRows').querySelectorAll('.mname')).some(e => /forgeboard|锻造台/i.test(e.textContent)) ? 'y' : 'N') +
    ' powerRows=' + $('powerRows').children.length +
    ' startupRows=' + $('startupRows').children.length + ' suBtns=' + $('startupRows').querySelectorAll('button').length +
    ' suPills=' + $('startupRows').querySelectorAll('span.stl').length +
    ' taskRows=' + $('taskRows').children.length + ' taskRes=' + $('taskRows').querySelectorAll('span.stl').length + ' tkState=' + String($('tkState').textContent || '').slice(0, 26) +
    ' tierBtns=' + Array.from($('mountedRows').querySelectorAll('tr')).filter(tr => /strata/.test(String(tr.textContent))).slice(0, 5).map(tr => {
      const b = tr.querySelector('.act-stop') || tr.querySelector('.act-start');
      const nm = (tr.querySelector('.mname') || {}).textContent || '?';
      return nm + '=' + (b ? b.textContent : '-') + (tr.className === 'running' ? '(run)' : '');
    }).join(' / ') +
    ' selfOn=' + (SNAP && SNAP.selfOn ? 'y' : 'no') + ' selfDir=' + (SNAP && SNAP.selfDir ? 'y' : 'N') +
    ' cronCells=' + $('cronRows').querySelectorAll('span.badge.switch').length +
    ' runModel=' + (((SNAP && SNAP.crons) || []).filter(c => c.runModel).length) +
    ' pinned=' + (((SNAP && SNAP.crons) || []).filter(c => c.model && !c.runModel).length) +
    ' schedTxt=' + Array.from($('cronRows').querySelectorAll('tr')).slice(0, 4).map(tr => String((tr.children[3] || {}).textContent || '').replace(/\s+/g, ' ')).join('/').slice(0, 70));
  const r0 = $('reserveRows').children.length ? $('reserveRows').children[0].textContent.replace(/\s+/g, ' ') : '';
  rep.push('reserveRows=' + $('reserveRows').children.length + ' reserveNow=' + r0.slice(0, 46) +
    ' reservePrefill=' + String($('reserveFirst').value || '') + '/' + String($('reserveLater').value || '') +
    ' reserveMatch=' + (r0.includes(String($('reserveFirst').value)) && String($('reserveFirst').value) !== '0' ? 'yes' : 'no') +
    ' cache=' + String($('cacheState').textContent || '').slice(0, 40));
  rep.push('hostCards=' + $('hostCards').children.length + ' hostTxt=' + String($('hostCards').textContent || '').slice(0, 60) + ' detectBtns=' + ['btnDetect', 'btnDetectApply'].map(i => ($(i) ? 'y' : 'N')).join('') + ' hbBtnGone=' + ($('btnHbSwitch') ? 'N' : 'y'));
  const u = (SNAP && SNAP.usage) || {};
  rep.push('usagePanel=' + ['usStats', 'usModelRows', 'usUnpriced', 'btnUsageNow', 'ckTkSys', 'stepOut'].map(i => ($(i) ? 'y' : 'NO')).join(',') +
    ' usageNote=' + String(u.note || '-') + ' usageWindows=' + (u.windows || 0) +
    ' usageModels=' + ((u.models || []).length) + ' usageToday=' + ((u.today || {}).tokens || 0) + ' usageTotals=' + ((u.totals || {}).tokens || 0) +
    ' usageChip=' + String($('tokChip') ? $('tokChip').textContent : '').slice(0, 26) +
    ' usageState=' + String($('usState') ? $('usState').textContent : '').slice(0, 40));
  rep.push('stepText=' + stepText('step step_big_kill tier=iq3x ports=8095').slice(0, 40) +
    ' stepTwoArgs=' + stepText('step step_svc_start port=8095 cmd=llama-server.exe').slice(0, 40) +
    ' tkHideCk=' + ($('ckTkSys') ? ($('ckTkSys').checked ? 'on' : 'off') : 'NO'));
  rep.push('svcBtns=' + ['btnGwRescue', 'btnGwRescueHard', 'btnStopEverything'].map(i => ($(i) ? 'y' : 'N')).join('') + ' svcActBtns=' + $('serviceRows').querySelectorAll('.svc-act').length + ' svcActOff=' + $('serviceRows').querySelectorAll('.svc-act[disabled]').length + ' svcLocCells=' + $('serviceRows').querySelectorAll('div.loc').length + ' reserveBtns=' + ['btnReserveSet', 'btnReserveRollback'].map(i => ($(i) ? 'y' : 'N')).join(''));
  rep.push('bigInModels=' + ($('btnRestartBig') && !!$('btnRestartBig').closest('#page-models') ? 'y' : 'NO') +
 ' bigInServices=' + ($('btnRestartBig') && !!$('btnRestartBig').closest('#page-services') ? 'YES' : 'no') +
 ' gwRestartGone=' + ($('btnGwRestart') ? 'N' : 'y') +
 ' bigWarnTitle=' + String($('btnRestartBig') ? $('btnRestartBig').title : '').slice(0, 30) +
  ' rescueTitle=' + String($('btnGwRescue') ? $('btnGwRescue').title : '').slice(0, 24) +
  ' rescueHardTitle=' + String($('btnGwRescueHard') ? $('btnGwRescueHard').title : '').slice(0, 24));
  const langBeforeOps = LANG;
  setLangQuiet('en');
  for (const [name, call] of [['stopDry', () => goApp().StopAll(true)], ['stopAllDry', () => goApp().StopEverything(true)], ['svcStopDry', () => goApp().ServiceAct('router', 8080, 'stop', true)], ['svcRestDry', () => goApp().ServiceAct('engine', 8095, 'rest', true)], ['svcStartDry', () => goApp().ServiceAct('engine', (SNAP && SNAP.swap && SNAP.swap.port) || 8089, 'start', true)], ['resDry', () => goApp().SetReserve(1536, 300, true)], ['rbDry', () => goApp().RollbackReserve(true)], ['cacheDry', () => goApp().ClearBigCache(true)], ['bigDry', () => goApp().RestartBigModel(true)], ['rescueDry', () => goApp().RescueGateway(false, true)], ['hardDry', () => goApp().RescueGateway(true, true)]]) {
    try { const v = await Promise.race([call().then(x => ({ txt: opText(x), head: opHead(x) })).catch(e => ({ txt: 'err:' + (e.message || e), head: 'err:' + (e.message || e) })), new Promise(r => setTimeout(() => r('TIMEOUT'), 20000))]); const badTok = /\b(op_|note_)[a-z_]+/.test(v.txt) ? ' TOKEN-LEAK' : ''; const zhInEn = /[\u4e00-\u9fff]/.test(v.head) ? ' CJK-IN-EN' : ''; rep.push(name + '=ok:' + v.txt.replace(/\s+/g, ' ').slice(0, 110) + badTok + zhInEn); }
    catch (e) { rep.push(name + '-threw ' + (e.message || e)); }
  }
  setLangQuiet(langBeforeOps);
  try {
    const pw = await Promise.race([
      goApp().SetPowerWatts(0, 0, true).then(v => 'ok:' + String(v).replace(/\s+/g, ' ').slice(0, 70)).catch(e => 'err:' + (e.message || e)),
      new Promise(res => setTimeout(() => res('TIMEOUT-8s'), 8000)),
    ]);
    rep.push('pwDry=' + pw);
  } catch (e) { rep.push('pwDry-threw ' + (e.message || e)); }
  setLangQuiet('en');
  if (SNAP) { renderTop(SNAP); }
  const extraEN = [$('curModel').textContent, $('monLink').textContent, $('refreshAt').textContent, $('ocLink').textContent].join(' | ');
  rep.push('extraEN=' + extraEN.replace(/\s+/g, ' ').slice(0, 110) + (/\b(op_|note_|cal_)[a-z_]+/.test(extraEN) ? ' TOKEN-LEAK' : '') + (/[\u4e00-\u9fff]/.test(extraEN) ? ' CJK-IN-EN' : ''));
  setLangQuiet(langBeforeOps);
  const meas = $('mountedRows').querySelector('.badge.measured');
  rep.push('measuredBadge=' + (meas ? meas.textContent : 'none'));
  const rows = [...$('mountedRows').querySelectorAll('tr')];
  rep.push('kindRows=' + $('mountedRows').querySelectorAll('.badge.kind').length +
    ' srcBadgeGone=' + ($('mountedRows').textContent.indexOf('read from the model file') < 0 && $('mountedRows').textContent.indexOf('读自模型文件') < 0 ? 'y' : 'N') +
    ' ownNames=' + rows.filter(r => { const k = r.querySelector('.mkey'); return k && k.textContent !== r.getAttribute('data-id'); }).length +
    ' idFallback=' + rows.filter(r => { const k = r.querySelector('.mkey'); return k && k.textContent === r.getAttribute('data-id'); }).length +
    ' zeroNeed=' + rows.filter(r => /(^|\D)0(\s|$)/.test((r.querySelector('.num') || {}).textContent || '')).length);
  const stopBtn = $('mountedRows').querySelector('.act-stop');
  rep.push('stopBtn=' + (stopBtn ? Math.round(stopBtn.getBoundingClientRect().width) + 'px text=' + stopBtn.textContent : 'none'));
  const before = SNAP ? SNAP.models.length : -1;
  const prevLang = LANG;
  setLangQuiet('en'); // 先切英文再切中文：当前语言本来就是中文时
  const tabsEN = [...document.querySelectorAll('.tab')].map(b => b.textContent).join('/');
  const tgENtxt = String($('engineState').textContent || '');
  setLangQuiet('zh');
  const tabsZH = [...document.querySelectorAll('.tab')].map(b => b.textContent).join('/');
  const tgZHtxt = String($('engineState').textContent || '');
  const colsZH = [...document.querySelectorAll('#modelTable th')].map(x => x.textContent).join('/');
  setLangQuiet(prevLang);
  const tabsBack = [...document.querySelectorAll('.tab')].map(b => b.textContent).join('/');
  rep.push('tabsEN=' + tabsEN);
  rep.push('tabsZH=' + tabsZH);
  rep.push('taglineEN=' + (/[\u4e00-\u9fff]/.test(tgENtxt) ? 'HAS-CJK' : 'clean') + ' taglineZH=' + (/[\u4e00-\u9fff]/.test(tgZHtxt) ? 'has-cjk' : 'NO-CJK'));
  rep.push('colsZH=' + colsZH);
  rep.push('tabsBackEN=' + tabsBack);
  const startBtn = $('mountedRows').querySelector('.act-start:not([disabled])') || $('mountedRows').querySelector('.act-start');
  const dk = startBtn ? startBtn.getAttribute('data-key') : '';
  try {
    const raced = await Promise.race([
      goApp().StartModel(dk, true).then(v => 'ok:' + opText(v).replace(/\s+/g, ' ').slice(0, 70)).catch(e => 'err:' + (e.message || e)),
      new Promise(res => setTimeout(() => res('TIMEOUT-4s'), 4000)),
    ]);
    rep.push('dryCall[' + dk + ']=' + raced);
  try {
    const rr = await Promise.race([
      goApp().ReloadSwap(true).then(v => ({ txt: opText(v), head: opHead(v) })).catch(e => ({ txt: 'err:' + (e.message || e), head: 'err:' + (e.message || e) })),
      new Promise(r => setTimeout(() => r('TIMEOUT-6s'), 6000)),
    ]);
    if (rr === 'TIMEOUT-6s') rep.push('reloadDry=TIMEOUT');
    else rep.push('reloadDry=ok:' + rr.txt.replace(/\s+/g, ' ').slice(0, 90) + (/\b(op_|note_)[a-z_]+/.test(rr.txt) ? ' TOKEN-LEAK' : '') + (/[\u4e00-\u9fff]/.test(rr.head) ? ' CJK-IN-EN' : ''));
  } catch (e) { rep.push('reloadDry-threw ' + (e.message || e)); }
  const entryTxt = String($('entryLine') ? $('entryLine').textContent : '');
  const entryPort = SNAP && SNAP.swap && SNAP.swap.port ? SNAP.swap.port : 'none';
  rep.push('reloadBtn=' + ($('btnReloadSwap') ? 'y' : 'NO') + ' entryLine=' + entryTxt.slice(0, 60) +
    ' entryPortReal=' + entryPort +
    ' entryMatchesReal=' + (entryPort !== 'none' && entryTxt.indexOf(':' + entryPort) >= 0 ? 'y' : 'NO') +
    ' entryHasOld8090=' + (entryTxt.indexOf('8090') >= 0 ? 'YES' : 'no'));
  } catch (e) { rep.push('dryCall-threw ' + (e.message || e)); }
  if (startBtn) {
    startBtn.click();
    let shown = false, tick = -1;
    for (let i = 1; i <= 12; i++) {
      await new Promise(r => setTimeout(r, 400));
      if (!$('confirm').classList.contains('hidden')) { shown = true; tick = i; break; }
    }
    rep.push('dialogShown=' + shown + ' afterMs=' + (tick * 400));
    rep.push('dialogShown=' + shown + ' title=' + ($('confirmTitle').textContent || '-') + ' dryLen=' + ($('confirmDry').textContent || '').length + ' dryHead=' + ($('confirmDry').textContent || '').replace(/\s+/g, ' ').trim().slice(0, 60));
    $('confirmNo').click();
    await new Promise(r => setTimeout(r, 300));
    rep.push('dialogClosed=' + $('confirm').classList.contains('hidden'));
  } else rep.push('dialogSkipped=noStartButton');
  showTab('sessions');
  rep.push('sessionRows=' + $('sessionRows').children.length + ' targets=' + $('targetModel').options.length);
  const firstKey = $('sessionRows').querySelector('input[type=checkbox]');
  if (firstKey) {
    const k = firstKey.getAttribute('data-key');
    const m = ($('targetModel').value || '');
    try {
      const raced = await Promise.race([
        goApp().SwitchWindows(m, [k], true).then(v => 'ok:' + String(v).replace(/\s+/g, ' ').slice(0, 90)).catch(e => 'err:' + (e.message || e)),
        new Promise(res => setTimeout(() => res('TIMEOUT-8s'), 8000)),
      ]);
      rep.push('winDry[' + m + ']=' + raced);
    } catch (e) { rep.push('winDry-threw ' + (e.message || e)); }
  } else rep.push('winDry=skipped');
  for (let i = 0; i < 4; i++) {
    await new Promise(r => setTimeout(r, 3000));
    await refresh();
    if ($('taskRows').querySelectorAll('tr').length > 1 && !$('taskRows').querySelector('.empty')) break;
  }
  rep.push('tasksSecond=' + $('taskRows').querySelectorAll('tr').length + ' res2=' + $('taskRows').querySelectorAll('span.stl').length +
    ' tkState2=' + String($('tkState').textContent || '').slice(0, 26) + ' flags2=' + $('taskRows').querySelectorAll('span.badge').length +
    ' sys2=' + $('taskRows').querySelectorAll('tr').length);
  showTab('models');
  try { await goApp().Debug('selftest ' + rep.join(' | ')); } catch (e) { rep.push('debug-call-failed ' + (e.message || e)); }
  $('doctorOut').textContent = rep.join('\n') + '\nmodelsBefore=' + before;
  showTab(prevTab);
  feed(t('selftest_done'));
}

// 自检跑在看不见的层里：页面不跳、语言不跳，跑完原样恢复。
async function selfTest() {
  const prevTab = TAB;
  document.body.classList.add('selftesting');
  const bar = $('busyBar');
  if (bar) { bar.textContent = t('selftest_running'); bar.classList.remove('hidden'); }
  if (window.FB_TIMER) { clearInterval(window.FB_TIMER); window.FB_TIMER = null; } // 跑期间不刷新，免得堆叠变慢
  try {
    await selfTestRun();
  } catch (e) {
    try { await goApp().Debug('selftest-threw ' + (e && (e.stack || e.message) || e)); } catch (e2) {}
    feed(t('err_prefix') + ((e && e.message) || e));
  } finally {
    document.body.classList.remove('selftesting');
    if (bar) bar.classList.add('hidden');
    applyI18n();
    if (SNAP) render(SNAP);
    showTab(prevTab);
    startAutoRefresh();
  }
}

async function boot() {
  LANG = getLang();
  try { const l = await goApp().LangNow(); if (l === 'zh' || l === 'en') LANG = l; } catch (e) { /* 桥没好就用浏览器里存的 */ }
  $('langSel').value = LANG;
  applyI18n();
  document.querySelectorAll('.tab').forEach(b => b.onclick = () => showTab(b.getAttribute('data-tab')));
  $('btnRefresh').onclick = refresh;
  $('btnStopAll').onclick = doStopAll;
  if ($('selGPU')) $('selGPU').onchange = (e) => { goApp().SetGPU(e.target.value).then((g) => { if (SNAP) SNAP.gpu = g; }).catch(() => {}); };
  $('ckHideSmall').onchange = (e) => { goApp().SetHideSmall(e.target.checked).catch(() => {}); if (SNAP) { renderMounted({ ...SNAP, hideSmall: e.target.checked }); } };
  $('btnSave').onclick = doSave;
  $('btnDetect').onclick = doDetect;
  $('btnDetectApply').onclick = doDetectApply;
  $('btnDoctor').onclick = doDoctor;
  $('btnShot').onclick = selfTest;
  $('btnSwitchSel').onclick = switchSelected;
  $('btnSelChat').onclick = selectChatWindows;
  $('btnGwRescue').onclick = () => askGwRescue(false);
  $('btnGwRescueHard').onclick = () => askGwRescue(true);
  $('btnRestartBig').onclick = askRestartBig;
  $('btnStopEverything').onclick = askStopEverything;
  $('btnClearCache').onclick = askClearCache;
  $('btnReloadSwap').onclick = askReloadSwap;
$('btnUsageNow').onclick = doUsageNow;
$('ckTkSys').onchange = () => { if (SNAP) renderTasks(SNAP); };
  $('btnReserveSet').onclick = askReserveSet;
  $('btnReserveRollback').onclick = askReserveRollback;
  $('btnSelfOn').onclick = () => askSelfStart(true);
  $('btnSelfOff').onclick = () => askSelfStart(false);
  $('btnOpenFolder').onclick = () => { goApp().OpenStartupFolder().catch(e => feed(t('err_prefix') + (e.message || e))); };
  $('confirmNo').onclick = closeConfirm;
  $('confirmYes').onclick = () => { const f = pending; closeConfirm(); if (f) f(); };
  $('langSel').onchange = (e) => { setLang(e.target.value); if (SNAP) render(SNAP); };

  try {
    const st = await goApp().StartTab();
    if (st === 'debug') { showTab('diag'); await refresh(); await selfTest(); }
    else if (st) showTab(st);
  } catch (e) { try { goApp().Debug('boot-threw ' + (e && (e.stack || e.message) || e)); } catch (e2) {} /* 桥没好时下面 refresh 会报出来 */ }

  await refresh();
  try { $('doctorOut').textContent = await goApp().DoctorText(); } catch (e) {}

  if (window.go && window.go.app && window.go.app.Events) {
    window.go.app.Events.On('feed', (msg) => feed(msg));
    window.go.app.Events.On('tab', (name) => showTab(name));
    window.go.app.Events.On('usage', () => { refresh(); });
  }
  startAutoRefresh();
}

document.addEventListener('DOMContentLoaded', boot);
