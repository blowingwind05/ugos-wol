// Development-only SDK/API fixtures; production authentication is unchanged.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('node:fs'),http=require('node:http'),path=require('node:path'),os=require('node:os'),assert=require('node:assert/strict');
const repo=path.resolve(__dirname,'../..');
const {build}=require(path.join(repo,'frontend/node_modules/esbuild'));
(async()=>{
 const root=path.join(repo,'backend/web'),temp=fs.mkdtempSync(path.join(os.tmpdir(),'ugos-wol-status-ui-')),bundle=path.join(temp,'app.js');
 const html=fs.readFileSync(path.join(root,'index.html'),'utf8');
 await build({entryPoints:[path.join(repo,'frontend/app.js')],outfile:bundle,bundle:true,format:'esm',plugins:[{name:'test-sdk',setup(b){b.onResolve({filter:/^@ugreen-nas\/core/},args=>({path:args.path,namespace:'fixture'}));b.onLoad({filter:/.*/,namespace:'fixture'},args=>({contents:args.path.endsWith('cloudWindow')?'export default {useCapacity:async()=>({third_token:"fixture-token"})}':'export default {init:async()=>{}}',loader:'js'}));}}]});
 const server=http.createServer((req,res)=>{
  const pathname=new URL(req.url,'http://local').pathname;
  if(pathname==='/host'){res.setHeader('Content-Type','text/html');res.end('<iframe title="UGOS" src="/" style="position:fixed;inset:0;border:0;width:100%;height:100%"></iframe>');return;}
  const file=/^\/app\.[a-f0-9]{12}\.js$/.test(pathname)?bundle:path.join(root,pathname==='/'?'index.html':pathname.slice(1));
  if(!file.startsWith(root+path.sep)&&file!==bundle){res.writeHead(403);res.end();return;}
  try{res.setHeader('Content-Type',file.endsWith('.js')?'text/javascript':file.endsWith('.css')?'text/css':file.endsWith('.svg')?'image/svg+xml':'text/html');res.end(fs.readFileSync(file));}catch{res.writeHead(404);res.end();}
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const opts={headless:true};if(process.env.BROWSER_EXECUTABLE)opts.executablePath=process.env.BROWSER_EXECUTABLE;
 const browser=await chromium.launch(opts),checks=[],errors=[];
 try{
  const page=await browser.newPage({viewport:{width:1600,height:900}});page.on('pageerror',e=>errors.push(e.message));await page.route('**/icons*.svg*',route=>route.abort());
  const network={id:'eth0/192.168.1.0/24',name:'eth0',ip:'192.168.1.2',cidr:'192.168.1.0/24',broadcast:'192.168.1.255'};
  const info={version:'0.5.0',devices:['windows','macos','linux',''].map((system,i)=>({id:String(i),name:['办公电脑','设计工作站','Linux 服务器','其他电脑'][i],mac:`02:11:22:33:44:0${i}`,network:'auto',os:system,ip:i?'':`192.168.1.20`})),networks:[network],history:[]};
  let statusMode='normal',posted,checksCount=0,wakes=0;
  await page.route('**/api/wol/v1/**',async route=>{
   const req=route.request(),key=new URL(req.url()).pathname.split('/v1/')[1];assert.equal(req.headers()['ugreen-ttk'],'fixture-token');
   if(key==='info')return route.fulfill({json:info});
   if(key==='status'){
    checksCount++;if(statusMode==='denied')return route.fulfill({status:403,json:{error:'仅管理员可访问'}});
    return route.fulfill({json:{devices:info.devices.map((d,i)=>({id:d.id,state:statusMode==='allOnline'?'online':['online','unresponsive','unknown','unknown'][i]||'unknown',ip:d.ip||'',reason:i===0?'所填 IP 有网络响应；可能经过无线中继，按 IP 检测':'示例网络探测结果',source:i===0?'ip':'mac',mac_verified:i!==0,checked_at:new Date().toISOString()}))}});
   }
   if(key==='scan')return route.fulfill({json:{devices:[{name:'新电脑',ip:'192.168.1.74',mac:'02:AA:BB:CC:DD:EE',network:network.id},{name:'新电脑',ip:'192.168.1.121',mac:'02:AA:BB:CC:DD:EE',network:network.id}],cidr:network.cidr,limited:false}});
   if(key.startsWith('devices')&&['POST','PUT'].includes(req.method())){
    posted=req.postDataJSON();if(posted.ip==='invalid')return route.fulfill({status:400,json:{error:'IP 地址需为所选 NAS 局域网内的有效 IPv4 设备地址'}});
    const id=key.split('/')[1]||'new';const existing=info.devices.find(d=>d.id===id);if(existing)Object.assign(existing,posted);else info.devices.push({id,...posted});
    return route.fulfill({json:{id,...posted}});
   }
   if(key.startsWith('wake/')){wakes++;return route.fulfill({json:{event:{sent:3},log_saved:true,message:'唤醒包已发送'}});}
   return route.fulfill({status:404,json:{error:'unknown'}});
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/host`);const f=page.frameLocator('iframe');
  await f.locator('.device-status.online').waitFor();assert.equal(await f.locator('.device-status.unresponsive').count(),1);assert.equal(await f.locator('.device-status.unknown').count(),2);checks.push('online, unresponsive and unknown remain distinct');
  assert((await f.locator('.device-status.online').first().getAttribute('title')).includes('无线中继'));await f.locator('.device-status.online').first().click();assert((await f.locator('#toast svg').getAttribute('data-icon')).endsWith('info-20'));assert.equal(await f.locator('.sidebar-bottom').innerText(),'v0.5.0');checks.push('explicit IP status works through a relay, details use an info icon, and version is 0.5.0');
  const icons=await f.locator('.device-art').evaluateAll(ns=>ns.map(n=>({href:n.getAttribute('data-icon'),width:n.getBBox().width,height:n.getBBox().height})));
  assert(icons.every(n=>n.width>0&&n.height>0));assert(icons[0].href.endsWith('os_windows-20'));assert(icons[1].href.endsWith('os_apple-20'));assert(icons[2].href.endsWith('os_linux-20'));assert(icons[3].href.endsWith('laptop-48'));checks.push('inline OS icons render with external sprites blocked; unspecified systems retain original artwork');
  await page.screenshot({path:path.join(repo,'docs/assets/status-desktop.png'),fullPage:true});
  await f.locator('.device-card').nth(3).locator('summary').click();await f.locator('.device-card').nth(3).locator('.edit').click();
  assert.equal(await f.locator('#device-ip').inputValue(),'');assert.equal(await f.locator('#device-os').inputValue(),'');assert(!(await f.locator('#device-ip').getAttribute('required')));checks.push('legacy devices open with optional IP and OS unset');
  await f.locator('#device-ip').fill('192.168.1.24');await f.locator('#device-os').selectOption('windows');assert((await f.locator('#os-preview svg').getAttribute('data-icon')).endsWith('os_windows-20'));
  await f.locator('#save-device').click();await f.locator('#device-dialog').waitFor({state:'hidden'});assert.equal(posted.ip,'192.168.1.24');assert.equal(posted.os,'windows');checks.push('optional IP and OS round-trip through edit and save');
  await f.locator('#add-device').click();assert.equal(await f.locator('#device-os').inputValue(),'');await f.locator('#device-name').fill('手填设备');await f.locator('#device-mac').fill('02:11:22:33:44:99');await f.locator('#device-ip').fill('invalid');await f.locator('#save-device').click();await f.locator('#device-error').filter({hasText:'有效 IPv4'}).waitFor();assert(await f.locator('#device-dialog').isVisible());checks.push('invalid IP reports inline and preserves form');
  await f.locator('#device-ip').fill('');await f.locator('#device-os').selectOption('other');await f.locator('#save-device').click();await f.locator('#device-dialog').waitFor({state:'hidden'});assert.equal(posted.ip,'');checks.push('manual addition without IP still works');
  await f.locator('#add-device').click();await f.locator('#device-mac').fill('02:AA:BB:CC:DD:01');await f.locator('#entry-scan').click();await f.locator('#scan-devices').click();await f.locator('.scan-result').nth(1).waitFor();assert.equal(await f.locator('.scan-result').count(),2);await f.locator('.scan-result').nth(1).click();assert.equal(await f.locator('#device-ip').inputValue(),'192.168.1.121');assert.equal(await f.locator('#device-mac').inputValue(),'02:AA:BB:CC:DD:01');assert(await f.locator('#scan-mac-note').isVisible());assert.equal(await f.locator('#device-os').inputValue(),'');checks.push('discovery preserves both IPs for one MAC, fills the selected IP, warns about relay MACs and preserves the manually entered real MAC');
  for(const width of [1280,768,390,320]){
   await page.setViewportSize({width,height:844});
   const sizes=await page.frames()[1].evaluate(()=>{const d=document.getElementById('device-dialog');return {page:document.documentElement.scrollWidth<=innerWidth,dialog:d.scrollWidth<=d.clientWidth,height:d.getBoundingClientRect().height<=innerHeight-20};});
   assert(sizes.page&&sizes.dialog&&sizes.height,`layout ${width}: ${JSON.stringify(sizes)}`);
   if(width===390){await page.setViewportSize({width:390,height:1100});await f.locator('#device-dialog').screenshot({path:path.join(repo,'docs/assets/status-form-mobile.png'),animations:'disabled'});}
  }checks.push('optional fields and icon preview fit desktop and 320px mobile dialog');
  await f.locator('#cancel-device').click();await page.setViewportSize({width:390,height:844});await page.screenshot({path:path.join(repo,'docs/assets/status-mobile.png'),fullPage:true});
  const before=checksCount;statusMode='allOnline';await f.locator('.wake').nth(4).click();await f.locator('.device-status.online').nth(4).waitFor();assert.equal(wakes,1);assert(checksCount>before);checks.push('wake sends independently of status and starts a fresh status check');
  statusMode='denied';await f.locator('#refresh').click();await f.locator('#session-title').filter({hasText:'无权访问'}).waitFor();checks.push('status endpoint authorization failure ends the session');
  assert.deepEqual(errors,[]);const report={checks,pageErrors:errors,validation:'Mocked SDK/API in isolated Chrome; does not send LAN or WOL packets.'};fs.writeFileSync(path.join(repo,'docs/STATUS-UI-VALIDATION.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));
 }finally{await browser.close();await new Promise(r=>server.close(r));fs.rmSync(temp,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1});
