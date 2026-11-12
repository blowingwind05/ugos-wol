import UGOSCore from '@ugreen-nas/core';
import cloudWindow from '@ugreen-nas/core/cloudWindow';

const $ = (id) => document.getElementById(id);
const iconSprite = typeof __WOL_ICON_SPRITE__ === 'undefined' ? './icons.svg' : __WOL_ICON_SPRITE__;
let connected = false, thirdToken = '', initialized = false, editing = '';
let model = {devices: [], history: [], networks: []};
let toastTimer;
let scanController = null, scanSerial = 0, scanSuggestedName = '';
const wakeStates = new Map();
const embedded = window.parent !== window || Boolean(window.UgWebHost);
function text(tag, content, className = '') { const el = document.createElement(tag); el.textContent = content; el.className = className; return el; }
function icon(name) {
  const ns = 'http://www.w3.org/2000/svg', svg = document.createElementNS(ns,'svg'), use = document.createElementNS(ns,'use');
  svg.classList.add('icon'); svg.setAttribute('aria-hidden','true'); use.setAttribute('href',`${iconSprite}#${name}-20`); svg.append(use); return svg;
}
function message(value, warning = false) { $('toast-message').textContent = value; $('toast').querySelector('use').setAttribute('href',`${iconSprite}#${warning ? 'warning' : 'checkmark_circle'}-20`); $('toast').classList.toggle('warning', warning); $('toast').hidden = false; clearTimeout(toastTimer); toastTimer = setTimeout(() => $('toast').hidden = true, 7500); }
function formatTime(time) { return new Date(time).toLocaleString('zh-CN', {month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit',second:'2-digit',hour12:false}); }
async function token(force = false) {
  if (!embedded) throw new Error('请从已登录的 UGOS 桌面或绿联客户端中打开本应用');
  if (thirdToken && !force) return thirdToken;
  if (!initialized) {
    await Promise.race([UGOSCore.init(), new Promise((_,reject) => setTimeout(() => reject(new Error('绿联应用初始化超时，请重新打开应用')), 6000))]);
    initialized = true;
  }
  const info = await cloudWindow.useCapacity('getThirdToken', undefined, 6000);
  if (!info?.third_token) throw new Error('无法获取绿联登录令牌，请从 UGOS 桌面重新打开应用');
  thirdToken = info.third_token;
  return thirdToken;
}
async function request(path, method = 'GET', body, retry = true, signal) {
  const headers = {};
  const ttk = await token(); if (ttk) headers['Ugreen-Ttk'] = ttk;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  if (signal?.aborted) throw new DOMException('请求已取消','AbortError');
  const controller = new AbortController(), abort = () => controller.abort();
  signal?.addEventListener('abort',abort,{once:true});
  const timeout = setTimeout(abort,15000);
  let response;
  try { response = await fetch(`/api/wol/v1/${path}`, {method, headers, body:body === undefined ? undefined : JSON.stringify(body), credentials:'same-origin', signal:controller.signal}); }
  catch { if (signal?.aborted) throw new DOMException('请求已取消','AbortError'); throw new Error('无法连接唤醒服务，请重试'); }
  finally { clearTimeout(timeout); signal?.removeEventListener('abort',abort); }
  if (response.status === 401 && embedded && retry) { await token(true); return request(path,method,body,false,signal); }
  let data;
  try { data = await response.json(); } catch {
    const error = new Error('UGOS 网关未返回有效身份数据，请重新登录 NAS 后打开应用');
    error.status = 401;
    throw error;
  }
  if (!response.ok) {
    const error = new Error(data.error || data.message || `请求失败 (${response.status})`);
    error.status = response.status; error.event = data.event;
    throw error;
  }
  return data;
}
async function refresh() { model = await request('info'); render(); }
function sessionFailed(error) {
  connected = false; thirdToken = '';
  for (const id of ['device-dialog','delete-dialog']) if ($(id).open) $(id).close();
  $('app').hidden = true; $('session-screen').hidden = false;
  const denied = error.status === 403;
  $('session-title').textContent = denied ? '无权访问' : error.status === 401 ? '需要重新验证身份' : '无法打开远程唤醒';
  $('session-message').textContent = error.message;
  $('retry-session').hidden = denied;
}
async function handle(action) { try { await action(); } catch (error) { if (error.status === 401 || error.status === 403) sessionFailed(error); message(error.message,true); } }
async function startSession() {
  $('retry-session').disabled = true; $('session-title').textContent = '正在连接 NAS'; $('session-message').textContent = '正在验证当前 UGOS 登录身份…';
  try { await refresh(); connected = true; $('session-screen').hidden = true; $('app').hidden = false; }
  catch (error) { sessionFailed(error); }
  finally { $('retry-session').disabled = false; }
}
$('retry-session').addEventListener('click',startSession);
function showView(name) {
  document.querySelectorAll('[data-view]').forEach(item => { const active = item.dataset.view === name; item.classList.toggle('active',active); if (active) item.setAttribute('aria-current','page'); else item.removeAttribute('aria-current'); });
  document.querySelectorAll('.view').forEach(item => item.hidden = item.id !== `view-${name}`);
  closeMenus();
}
document.querySelectorAll('[data-view]').forEach(button => button.addEventListener('click', () => showView(button.dataset.view)));
document.querySelectorAll('[data-open-view]').forEach(button => button.addEventListener('click', () => { showView(button.dataset.openView); window.scrollTo(0,0); }));
function closeMenus(except) { document.querySelectorAll('.card-menu[open]').forEach(menu => { if (menu !== except) menu.open = false; }); }
document.addEventListener('click',event => { const menu = event.target.closest('.card-menu'); closeMenus(menu); });
document.addEventListener('keydown',event => { if (event.key === 'Escape') { const open = document.querySelector('.card-menu[open]'); if (open) { open.open = false; open.querySelector('summary').focus(); } } });
function setWakeButton(button,state) {
  const names = {sending:['arrow_clockwise','发送中…'],sent:['checkmark_circle','已发送'],error:['warning','发送失败']};
  const [name,label] = names[state] || ['power','唤醒'];
  const glyph = icon(name); if (state === 'sending') glyph.classList.add('is-spinning');
  button.replaceChildren(glyph,text('span',label)); button.disabled = Boolean(state); button.setAttribute('aria-busy',String(state === 'sending'));
}
function wakeState(id,state) {
  if (state) wakeStates.set(id,state); else wakeStates.delete(id);
  document.querySelectorAll('.wake').forEach(button => { if (button.dataset.deviceId === id) setWakeButton(button,state); });
}
function render() {
  $('device-badge').textContent = model.devices.length;
  $('empty').hidden = model.devices.length > 0;
  $('devices').replaceChildren();
  model.devices.forEach(device => {
    const node = $('device-template').content.cloneNode(true);
    node.querySelector('.device-title').textContent = device.name;
    node.querySelector('.mac').textContent = device.mac;
    const network = model.networks.find(item => item.id === device.network);
    node.querySelector('.device-network').textContent = device.network === 'auto' ? '发送接口：自动' : network ? `发送接口：${network.name} · ${network.cidr}` : '接口已失效，请编辑设备重新选择';
    node.querySelector('.device-network').classList.toggle('invalid',device.network !== 'auto' && !network);
    node.querySelector('summary').setAttribute('aria-label',`${device.name}的设备操作`);
    const last = model.history.find(item => item.mac === device.mac);
    node.querySelector('.last-event').textContent = last ? `${formatTime(last.time)} ${last.sent ? last.error ? '部分发送' : '已发送' : '发送失败'}` : '尚未发送';
    node.querySelector('.edit').addEventListener('click', () => openDevice(device));
    node.querySelector('.delete').addEventListener('click', () => removeDevice(device));
    node.querySelector('.wake').addEventListener('click', (event) => wake(device, event.currentTarget));
    node.querySelector('.wake').dataset.deviceId = device.id;
    setWakeButton(node.querySelector('.wake'),wakeStates.get(device.id));
    $('devices').append(node);
  });
  $('history').replaceChildren();
  if (!model.history.length) $('history').append(text('p','尚无发送记录。添加设备并点击“唤醒”后，记录会出现在这里。','history-empty'));
  model.history.forEach(event => {
    const row = text('article','',`event${!event.sent ? ' failed' : ''}`);
    row.classList.toggle('failed',!event.sent || Boolean(event.error));
    const device = text('div','','event-device'), identity = document.createElement('div');
    identity.append(text('h3',event.name),text('p',event.mac,'event-mac mono')); device.append(icon('desktop'),identity);
    const status = text('div','','event-status'); status.append(icon(event.sent && !event.error ? 'checkmark_circle' : 'warning'),document.createTextNode(event.sent ? event.error ? '部分发送' : '已发送' : '发送失败'));
    const time = text('time',formatTime(event.time),'event-time'); time.dateTime = event.time;
    const actor = text('div',event.actor ? event.actor.name : '旧版记录 · 未记录','event-actor'); actor.title = event.actor ? `操作人：${event.actor.name}` : '旧版记录 · 未记录操作人';
    row.append(device,time,actor,status);
    const extra = text('div','','event-extra');
    if (event.error) extra.append(text('p',event.error,'event-error'));
    const details = document.createElement('details'); details.append(text('summary','查看发送详情'),text('p',`${event.sent} 个数据包`));
    if (event.targets?.length) details.append(text('p',event.targets.join(' / ')));
    extra.append(details); row.append(extra);
    $('history').append(row);
  });
  $('networks').replaceChildren();
  if (!model.networks.length) $('networks').append(text('p','未找到支持广播的 IPv4 局域网接口，请检查 NAS 网络连接或应用网络权限。','error'));
  model.networks.forEach(network => { const row = text('div','','network-row'), info = text('div','','network-info'); info.append(text('strong',network.name),text('span',`NAS ${network.ip} · ${network.cidr}`)); row.append(icon('server'),info,text('code',`广播 ${network.broadcast}:9`)); $('networks').append(row); });
}
function stopDiscovery() {
  scanSerial++;
  scanController?.abort(); scanController = null;
  $('scan-devices').disabled = !model.networks.length;
  $('scan-devices').setAttribute('aria-busy','false');
  $('scan-devices').querySelector('svg').classList.remove('is-spinning');
  $('scan-devices').querySelector('span').textContent = '扫描';
  $('scan-network').disabled = !model.networks.length;
}
function clearDiscovery() {
  stopDiscovery();
  $('scan-results').replaceChildren(); $('scan-results').hidden = true;
  $('scan-status').textContent = ''; $('scan-status').classList.remove('error');
}
function showDeviceEntry(mode) {
  const scanning = mode === 'scan';
  $('device-discovery').hidden = !scanning; $('device-fields').hidden = scanning;
  $('save-device').hidden = scanning;
  for (const id of ['device-name','device-mac','device-network']) $(id).disabled = scanning;
  for (const name of ['manual','scan']) {
    const active = name === mode, tab = $(`entry-${name}`);
    tab.setAttribute('aria-selected',String(active)); tab.tabIndex = active ? 0 : -1;
  }
}
for (const mode of ['manual','scan']) {
  $(`entry-${mode}`).addEventListener('click', () => showDeviceEntry(mode));
  $(`entry-${mode}`).addEventListener('keydown', event => {
    if (!['ArrowLeft','ArrowRight','Home','End'].includes(event.key)) return;
    event.preventDefault();
    const next = event.key === 'Home' ? 'manual' : event.key === 'End' ? 'scan' : mode === 'manual' ? 'scan' : 'manual';
    showDeviceEntry(next); $(`entry-${next}`).focus();
  });
}
function resetDiscovery() {
  clearDiscovery(); scanSuggestedName = '';
  $('device-entry-tabs').hidden = Boolean(editing);
  showDeviceEntry('manual');
  $('scan-network').replaceChildren();
  model.networks.forEach(network => $('scan-network').add(new Option(`${network.name} · ${network.cidr}`,network.id)));
  if (!model.networks.length) {
    $('scan-network').add(new Option('没有可用的局域网接口',''));
    $('scan-status').textContent = '无法扫描，可切换到手动填写设备信息。';
  }
}
function showDiscoveredDevices(result) {
  const list = $('scan-results'); list.replaceChildren();
  result.devices.forEach(device => {
    const row = document.createElement('li'), button = document.createElement('button');
    const saved = model.devices.some(item => item.mac.toUpperCase() === device.mac.toUpperCase());
    button.type = 'button'; button.className = 'scan-result'; button.disabled = saved;
    const label = device.name || device.ip;
    button.setAttribute('aria-label',saved ? `${label}，${device.ip}，已添加` : `选择 ${label}，IP ${device.ip}，MAC ${device.mac}`);
    const identity = text('span','','scan-identity');
    identity.append(text('strong',label));
    if (device.name) identity.append(text('span',device.ip,'mono'));
    identity.append(text('span',device.mac,'mono'));
    button.append(icon('desktop'),identity,text('span',saved ? '已添加' : '选择','scan-action'));
    button.addEventListener('click', () => {
      if (!model.networks.some(network => network.id === device.network)) {
        $('scan-status').textContent = '扫描接口已不可用，请重新打开添加设备窗口。';
        return;
      }
      $('device-mac').value = device.mac;
      $('device-network').value = device.network;
      if (!$('device-name').value.trim() || $('device-name').value === scanSuggestedName) {
        scanSuggestedName = device.name ? [...device.name].slice(0,40).join('') : `设备 ${device.ip}`; $('device-name').value = scanSuggestedName;
      }
      $('device-error').textContent = '';
      list.querySelectorAll('.scan-result').forEach(item => { item.classList.remove('selected'); item.removeAttribute('aria-pressed'); });
      button.classList.add('selected'); button.setAttribute('aria-pressed','true');
      showDeviceEntry('manual');
      $('device-name').focus(); $('device-name').select();
    });
    row.append(button); list.append(row);
  });
  list.hidden = !result.devices.length;
  const allSaved = result.devices.length && result.devices.every(device => model.devices.some(item => item.mac.toUpperCase() === device.mac.toUpperCase()));
  const found = !result.devices.length ? '未发现可选设备，请确认目标已开机联网，或切换到手动填写。' : allSaved ? `发现的 ${result.devices.length} 个设备均已添加。` : `发现 ${result.devices.length} 个设备，选择一项即可填写。`;
  $('scan-status').textContent = `${found} 扫描范围：${result.cidr}。${result.limited ? '网段较大，仅探测 NAS 所在的 /24 范围；其他范围的设备请手动添加。' : ''}`;
}
async function scanDevices() {
  if (scanController || !model.networks.length) return;
  clearDiscovery();
  const serial = scanSerial, controller = new AbortController(); scanController = controller;
  $('scan-devices').disabled = true; $('scan-network').disabled = true;
  $('scan-devices').setAttribute('aria-busy','true');
  $('scan-devices').querySelector('svg').classList.add('is-spinning');
  $('scan-devices').querySelector('span').textContent = '扫描中';
  $('scan-status').textContent = 'NAS 正在扫描并查询设备名称，请稍候…';
  try {
    const result = await request('scan','POST',{network:$('scan-network').value},true,controller.signal);
    if (serial === scanSerial && $('device-dialog').open) showDiscoveredDevices(result);
  } catch (error) {
    if (serial !== scanSerial || controller.signal.aborted) return;
    $('scan-status').textContent = error.message; $('scan-status').classList.add('error');
    if (error.status === 401 || error.status === 403) sessionFailed(error);
  } finally {
    if (serial === scanSerial) stopDiscovery();
  }
}
$('scan-devices').addEventListener('click',scanDevices);
$('scan-network').addEventListener('change',clearDiscovery);
$('device-dialog').addEventListener('close',stopDiscovery);
function openDevice(device = null) {
  closeMenus();
  editing = device?.id || ''; $('dialog-title').textContent = editing ? '编辑设备' : '添加设备';
  $('device-name').value = device?.name || ''; $('device-mac').value = device?.mac || ''; $('device-error').textContent = '';
  $('device-network').replaceChildren(new Option('自动 · 所有可用局域网接口', 'auto'));
  model.networks.forEach(network => $('device-network').add(new Option(`${network.name} · ${network.cidr}`,network.id)));
  if (device && device.network !== 'auto' && !model.networks.some(item => item.id === device.network)) $('device-network').add(new Option('原接口已失效，请重新选择',device.network));
  $('device-network').value = device?.network || 'auto';
  resetDiscovery();
  $('device-dialog').showModal();
}
$('add-device').addEventListener('click',() => openDevice()); $('add-first').addEventListener('click',() => openDevice());
const closeDialog = () => $('device-dialog').close();
$('close-dialog').addEventListener('click',closeDialog); $('cancel-device').addEventListener('click',closeDialog);
$('device-form').addEventListener('submit', async (event) => {
  event.preventDefault(); if ($('device-fields').hidden) return;
  $('save-device').disabled = true; $('device-error').textContent = '';
  const input = {name:$('device-name').value,mac:$('device-mac').value,network:$('device-network').value};
  try { await request(editing ? `devices/${editing}` : 'devices',editing ? 'PUT' : 'POST',input); closeDialog(); await refresh(); message('设备已保存'); }
  catch (error) { $('device-error').textContent = error.message; if (error.status === 401 || error.status === 403) sessionFailed(error); }
  finally { $('save-device').disabled = false; }
});
function removeDevice(device) {
  closeMenus();
  $('delete-message').textContent = `删除“${device.name}”的唤醒配置？已有发送记录仍会保留。`;
  const dialog = $('delete-dialog'); dialog.returnValue = '';
  dialog.addEventListener('close', () => { if (dialog.returnValue === 'confirm') handle(async () => { await request(`devices/${device.id}`,'DELETE'); await refresh(); message('设备已删除'); }); }, {once:true});
  dialog.showModal();
}
async function wake(device, button) {
  if (wakeStates.has(device.id)) return;
  wakeState(device.id,'sending');
  try { const result = await request(`wake/${device.id}`,'POST',{}); wakeState(device.id,result.event.error || !result.event.sent ? 'error' : 'sent'); message(result.message,Boolean(result.event.error) || !result.log_saved); }
  catch (error) { wakeState(device.id,'error'); message(error.message,true); if (error.status === 401 || error.status === 403) sessionFailed(error); }
  finally { await new Promise(resolve => setTimeout(resolve,3000)); wakeState(device.id,null); if (connected) await handle(refresh); }
}
$('refresh').addEventListener('click', () => handle(async () => { $('refresh').disabled = true; try { await refresh(); message('设备信息已刷新'); } finally { $('refresh').disabled = false; } }));
document.addEventListener('visibilitychange', () => { if (!document.hidden && connected) handle(refresh); });
setInterval(() => { if (!document.hidden && connected) refresh().catch(error => { if (error.status === 401 || error.status === 403) sessionFailed(error); else message(error.message,true); }); },60000);
startSession();
