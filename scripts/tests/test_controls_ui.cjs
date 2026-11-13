// Development-only SDK/API fixtures; production authentication is unchanged.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('node:fs'),http=require('node:http'),path=require('node:path'),os=require('node:os'),assert=require('node:assert/strict');
const repo=path.resolve(__dirname,'../..');
const {build}=require(path.join(repo,'frontend/node_modules/esbuild'));
(async()=>{
 const root=path.join(repo,'backend/web'),temp=fs.mkdtempSync(path.join(os.tmpdir(),'ugos-wol-tasks-ui-')),bundle=path.join(temp,'app.js');
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
  const page=await browser.newPage({viewport:{width:1440,height:960}});page.on('pageerror',e=>errors.push(e.message));await page.route('**/icons*.svg*',route=>route.abort());
  const network={id:'eth0/192.168.1.0/24',name:'eth0',ip:'192.168.1.2',cidr:'192.168.1.0/24',broadcast:'192.168.1.255'};
  const info={version:'0.5.0',devices:[{id:'pc',name:'办公电脑',mac:'02:11:22:33:44:55',network:'auto'}],tasks:[],history:[],networks:[network],default_timezone:'Asia/Shanghai',server_time:new Date().toISOString()};
  let posted,mode='normal',serial=0,wakes=0;
  const inputFor=body=>({...body,name:body.name||'办公电脑定时唤醒',date:body.repeat==='once'?body.date:undefined,weekdays:{daily:[1,2,3,4,5,6,7],weekdays:[1,2,3,4,5],weekends:[6,7]}[body.repeat]||body.weekdays});
  await page.route('**/api/wol/v1/**',async route=>{
   const req=route.request(),key=new URL(req.url()).pathname.split('/v1/')[1];assert.equal(req.headers()['ugreen-ttk'],'fixture-token');
   if(key==='info')return route.fulfill({json:info});
   if(key==='status')return route.fulfill({json:{devices:[]}});
   if(key.startsWith('wake/')){wakes++;return route.fulfill({json:{event:{sent:3},log_saved:true}});}
   if(key==='tasks'||key.startsWith('tasks/')){
    if(mode==='denied')return route.fulfill({status:403,json:{error:'仅管理员可管理定时任务'}});
    if(mode==='failure')return route.fulfill({status:500,json:{error:'任务保存失败，请检查数据目录'}});
    const id=key.split('/')[1];
    if(req.method()==='DELETE'){info.tasks=info.tasks.filter(t=>t.id!==id);return route.fulfill({json:{ok:true}});}
    posted=req.postDataJSON();
    if(posted.repeat==='once'&&posted.enabled&&posted.date<'2026-01-01')return route.fulfill({status:400,json:{error:'一次性任务的时间需晚于当前 NAS 时间'}});
    const task={...inputFor(posted),id:id||'task-'+(++serial),author:{name:'示例管理员'},next_run:posted.enabled?'2099-06-01T00:30:00Z':undefined};
    const index=info.tasks.findIndex(t=>t.id===id);if(index>=0)info.tasks[index]=task;else info.tasks.push(task);
    return route.fulfill({json:task});
   }
   return route.fulfill({status:404,json:{error:'unknown'}});
  });
  await page.goto(`http://127.0.0.1:${server.address().port}/host`);const f=page.frameLocator('iframe');await f.locator('#app').waitFor({state:'visible'});

  const choose=async(id,label)=>{await f.locator('#'+id+'-control').click();await f.locator('.control-popup [role=option]').filter({hasText:label}).first().click();};
  await f.locator('#add-device').click();await choose('device-os','Linux');assert.equal(await f.locator('#device-os').inputValue(),'linux');
  await f.locator('#device-os-control').click();assert.equal(await f.locator('.control-popup [role=option]').count(),5);assert.equal(await f.locator('.control-popup use').count(),0);await f.locator('#device-dialog').screenshot({path:path.join(repo,'docs/assets/controls-system.png')});
  await page.keyboard.press('Escape');assert(await f.locator('#device-dialog').isVisible());assert.equal(await f.locator('.control-popup').count(),0);
  await f.locator('#device-network-control').click();await page.keyboard.press('End');await page.keyboard.press('Enter');assert.equal(await f.locator('#device-network').inputValue(),network.id);checks.push('system icons, keyboard selection and Escape only closes a menu');
  await f.locator('#entry-scan').click();await f.locator('#scan-network-control').click();assert.equal(await f.locator('.control-popup [role=option]').count(),1);await page.keyboard.press('Escape');await f.locator('#cancel-device').click();checks.push('scan network uses the shared dropdown');
  await f.locator('[data-view=tasks]').click();await f.locator('#add-task').click();await choose('task-device','办公电脑');await choose('task-repeat','自定义时间');assert(await f.locator('#task-weekdays').isVisible());
  await f.locator('#task-time').fill('9:45');await f.locator('#task-time').press('Tab');assert.equal(await f.locator('#task-time').inputValue(),'09:45');
  await f.locator('#task-time').fill('24:60');await f.locator('#task-time').press('Tab');assert((await f.locator('#task-clock-error').innerText()).includes('有效时间'));await f.locator('#save-task').click();assert.equal(info.tasks.length,0);checks.push('typed time is normalized and invalid clock values cannot submit');
  await f.locator('#task-time').fill('08:30');await f.locator('#task-time').press('Tab');await f.locator('#task-time-trigger').click();await f.locator('.clock-column').nth(0).locator('[data-number="10"]').click();await f.locator('.clock-column').nth(1).locator('[data-number="15"]').click();await f.locator('.clock-actions .primary').click();assert.equal(await f.locator('#task-time').inputValue(),'10:15');checks.push('two-column clock selection updates the typed input');
  await f.locator('#task-more').click();await f.locator('#task-timezone-control').click();await f.locator('#task-dialog').screenshot({path:path.join(repo,'docs/assets/controls-timezone.png')});await f.locator('input[aria-label="搜索时区"]').fill('纽约');assert.equal(await f.locator('.control-popup [role=option]').count(),1);await f.locator('.control-popup [role=option]').click();assert.equal(await f.locator('#task-timezone').inputValue(),'America/New_York');assert((await f.locator('#task-zone-help').innerText()).includes('纽约'));checks.push('searchable timezone saves its IANA value and updates the displayed zone');
  await choose('task-repeat','仅一次');await f.locator('#task-date').fill('2026-02-30');await f.locator('#task-date').press('Tab');assert.equal(await f.locator('#task-date').evaluate(n=>n.checkValidity()),false);await f.locator('#task-date').fill('2099-06-01');await f.locator('#task-date-trigger').click();await f.locator('.date-grid button').filter({hasText:/^20$/}).click();assert.equal(await f.locator('#task-date').inputValue(),'2099-06-20');checks.push('calendar and typed dates share validation, including impossible dates');
  await f.locator('#task-enabled').uncheck();await f.locator('#save-task').click();await f.locator('#task-dialog').waitFor({state:'hidden'});assert.equal(posted.time,'10:15');assert.equal(posted.timezone,'America/New_York');assert.equal(posted.date,'2099-06-20');assert.equal(posted.enabled,false);await f.locator('.task-edit').click();assert.equal(await f.locator('#task-time').inputValue(),'10:15');await f.locator('#cancel-task').click();checks.push('enhanced controls round-trip through the existing task API');
  for(const width of [320,390,768,1440]){
    await page.setViewportSize({width,height:900});await f.locator('[data-view=devices]').click();await f.locator('#add-device').click();await f.locator('#device-os-control').click();
    const bounds=await page.frames()[1].evaluate(()=>{const el=document.querySelector('.control-popup'),r=el.getBoundingClientRect(),d=document.querySelector('#device-dialog');return {popup:r.left>=0&&r.right<=innerWidth&&r.top>=0&&r.bottom<=innerHeight,dialog:d.scrollWidth<=d.clientWidth,page:document.documentElement.scrollWidth<=innerWidth};});assert(bounds.popup&&bounds.dialog&&bounds.page,JSON.stringify({width,bounds}));
    if(width===390)await page.screenshot({path:path.join(repo,'docs/assets/controls-mobile.png')});await page.keyboard.press('Escape');await f.locator('#cancel-device').click();
  }checks.push('custom menus remain in the viewport at 320, 390, 768 and 1440px');
  await page.setViewportSize({width:960,height:600});await f.locator('[data-view=tasks]').click();await f.locator('#add-task').click();await f.locator('#task-more').click();
  const short=await f.locator('#save-task').evaluate(n=>{const r=n.getBoundingClientRect();return {top:r.top,bottom:r.bottom,height:innerHeight};});assert(short.top>=0&&short.bottom<=short.height,JSON.stringify(short));
  await page.screenshot({path:path.join(repo,'docs/assets/controls-short-window.png')});await f.locator('#cancel-task').click();checks.push('save and cancel stay visible when advanced settings are expanded in a 600px-high window');
  assert.equal(wakes,0);assert.deepEqual(errors,[]);const report={checks,pageErrors:errors,validation:'Actual frontend with mocked UGOS SDK/API; external SVG sprites blocked; no LAN or WOL packets sent.'};fs.writeFileSync(path.join(repo,'docs/CONTROLS-UI-VALIDATION.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));
 }finally{await browser.close();await new Promise(r=>server.close(r));fs.rmSync(temp,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1});
