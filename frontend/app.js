import UGOSCore from '@ugreen-nas/core';
import cloudWindow from '@ugreen-nas/core/cloudWindow';

const $ = (id) => document.getElementById(id);
let connected = false, thirdToken = '', initialized = false, editing = '';
let model = {devices: [], history: [], networks: []};
let toastTimer;
const wakeStates = new Map();
const embedded = window.parent !== window || Boolean(window.UgWebHost);
function text(tag, content, className = '') { const el = document.createElement(tag); el.textContent = content; el.className = className; return el; }
function icon(name) {
  const ns = 'http://www.w3.org/2000/svg', svg = document.createElementNS(ns,'svg'), use = document.createElementNS(ns,'use');
  svg.classList.add('icon'); svg.setAttribute('aria-hidden','true'); use.setAttribute('href',`./icons.svg#${name}-20`); svg.append(use); return svg;
}
function message(value, warning = false) { $('toast-message').textContent = value; $('toast').querySelector('use').setAttribute('href',`./icons.svg#${warning ? 'warning' : 'checkmark_circle'}-20`); $('toast').classList.toggle('warning', warning); $('toast').hidden = false; clearTimeout(toastTimer); toastTimer = setTimeout(() => $('toast').hidden = true, 7500); }
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
async function request(path, method = 'GET', body, retry = true) {
  const headers = {};
  const ttk = await token(); if (ttk) headers['Ugreen-Ttk'] = ttk;
  if (body !== undefined) headers['Content-Type'] = 'application/json';
  let response;
  try { response = await fetch(`/api/wol/v1/${path}`, {method, headers, body:body === undefined ? undefined : JSON.stringify(body), credentials:'same-origin', signal:AbortSignal.timeout(15000)}); }
  catch { throw new Error('无法连接唤醒服务，请重试'); }
  if (response.status === 401 && embedded && retry) { await token(true); return request(path,method,body,false); }
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
  $('session-title').textContent = '无法连接 NAS';
  $('session-message').textContent = error.message;
  $('retry-session').hidden = false;
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
function openDevice(device = null) {
  closeMenus();
  editing = device?.id || ''; $('dialog-title').textContent = editing ? '编辑设备' : '添加设备';
  $('device-name').value = device?.name || ''; $('device-mac').value = device?.mac || ''; $('device-error').textContent = '';
  $('device-network').replaceChildren(new Option('自动 · 所有可用局域网接口', 'auto'));
  model.networks.forEach(network => $('device-network').add(new Option(`${network.name} · ${network.cidr}`,network.id)));
  if (device && device.network !== 'auto' && !model.networks.some(item => item.id === device.network)) $('device-network').add(new Option('原接口已失效，请重新选择',device.network));
  $('device-network').value = device?.network || 'auto';
  $('device-dialog').showModal();
}
$('add-device').addEventListener('click',() => openDevice()); $('add-first').addEventListener('click',() => openDevice());
const closeDialog = () => $('device-dialog').close();
$('close-dialog').addEventListener('click',closeDialog); $('cancel-device').addEventListener('click',closeDialog);
$('device-form').addEventListener('submit', async (event) => {
  event.preventDefault(); $('save-device').disabled = true; $('device-error').textContent = '';
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
