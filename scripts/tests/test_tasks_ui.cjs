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
  await f.locator('[data-view=tasks]').click();await f.locator('#tasks-empty').waitFor({state:'visible'});assert.equal(await f.locator('.nav').count(),4);
  const nav=await f.locator('.nav svg[data-icon]').evaluateAll(nodes=>nodes.map(n=>({w:n.getBBox().width,h:n.getBBox().height})));assert(nav.every(n=>n.w>0&&n.h>0));checks.push('four aligned navigation entries including a library calendar icon');
  await f.locator('#add-task').click();assert.equal(await f.locator('#task-device').inputValue(),'pc');assert.equal(await f.locator('#task-timezone').inputValue(),'Asia/Shanghai');assert.equal(await f.locator('#task-repeat option').count(),5);assert(await f.locator('#task-date-field').isHidden());checks.push('device selection, explicit NAS default timezone and five repeat options');
  if(await f.locator('#task-advanced').isHidden())await f.locator('#task-more').click();await f.locator('#task-name').fill('工作日开机');await f.locator('#task-time').fill('08:30');await f.locator('#save-task').click();await f.locator('#task-dialog').waitFor({state:'hidden'});assert.equal(posted.repeat,'weekdays');assert.equal(posted.time,'08:30');assert.equal(posted.timezone,'Asia/Shanghai');await f.locator('.task-card').first().filter({hasText:'周一至周五'}).waitFor();checks.push('workday task saves and displays its rule and next run');
  await f.locator('.task-card').first().locator('.task-edit').click();await f.locator('#task-repeat').selectOption('weekends');await f.locator('#task-time').fill('10:15');if(await f.locator('#task-advanced').isHidden())await f.locator('#task-more').click();await f.locator('#task-timezone').selectOption('UTC');await f.locator('#save-task').click();await f.locator('#task-dialog').waitFor({state:'hidden'});assert.equal(posted.repeat,'weekends');assert.equal(posted.time,'10:15');await f.locator('.task-card').first().filter({hasText:'周六、周日'}).waitFor();checks.push('edit supports weekends, a custom clock time and a saved timezone');
  await f.locator('.task-toggle').first().click();await f.locator('.task-toggle[aria-checked=false]').first().waitFor();assert.equal(info.tasks[0].enabled,false);assert.equal(info.tasks[0].next_run,undefined);await f.locator('.task-toggle').first().click();await f.locator('.task-toggle[aria-checked=true]').first().waitFor();checks.push('enable and disable updates server state without sending WOL');
  info.history=[{name:'办公电脑',mac:'02:11:22:33:44:55',time:new Date().toISOString(),sent:3,targets:['eth0'],actor:{name:'示例管理员'},task_id:info.tasks[0].id,task_name:'工作日开机'}];await f.locator('[data-view=history]').click();await f.locator('.event-actor').filter({hasText:'定时'}).waitFor();await f.locator('.event-extra details summary').click();await f.locator('.event-extra').filter({hasText:'触发方式：定时任务'}).waitFor();await f.locator('[data-view=tasks]').click();checks.push('history refreshes on entry and identifies scheduled task triggers');
  await f.locator('#add-task').click();await f.locator('#task-repeat').selectOption('custom');for(const box of await f.locator('[name=weekday]').all())await box.uncheck();await f.locator('#save-task').click();await f.locator('#task-form-error').filter({hasText:'至少选择一天'}).waitFor();
  await f.locator('[name=weekday][value="5"]').check();await f.locator('[name=weekday][value="7"]').check();if(await f.locator('#task-advanced').isHidden())await f.locator('#task-more').click();await f.locator('#task-name').fill('周五和周日');await f.locator('#task-time').fill('19:45');await f.locator('#save-task').click();await f.locator('#task-dialog').waitFor({state:'hidden'});assert.deepEqual(posted.weekdays,[5,7]);assert.equal(posted.time,'19:45');checks.push('custom weekdays require a selection and preserve selected days and time');
  await f.locator('#add-task').click();await f.locator('#task-repeat').selectOption('once');assert(await f.locator('#task-date-field').isVisible());await f.locator('#task-date').fill('2000-01-01');await f.locator('#task-time').fill('23:45');await f.locator('#save-task').click();await f.locator('#task-form-error').filter({hasText:'晚于当前 NAS 时间'}).waitFor();assert(await f.locator('#task-dialog').isVisible());await f.locator('#task-date').fill('2099-06-01');await f.locator('#task-enabled').uncheck();await f.locator('#save-task').click();await f.locator('#task-dialog').waitFor({state:'hidden'});assert.equal(posted.repeat,'once');assert.equal(posted.date,'2099-06-01');assert.equal(posted.enabled,false);checks.push('one-shot date/time, past-time error and saving a disabled task');
  info.tasks[0].name='<img src=x onerror=alert(1)>';info.tasks[0].last_run={status:'failed',message:'无法使用发送接口',at:'2026-11-13T00:30:00Z'};info.task_error='无法保存任务执行状态，未发送唤醒包';await f.locator('#refresh-tasks').click();await f.locator('#task-service-error').waitFor({state:'visible'});assert.equal(await f.locator('#tasks img').count(),0);await f.locator('.task-last.error').first().click();await f.locator('#toast-message').filter({hasText:'无法使用发送接口'}).waitFor();checks.push('task names are escaped and scheduler/last-run errors remain visible');
  info.tasks[0].name='周末开机';info.tasks[0].last_run={status:'sent',sent:3,message:'唤醒包已发送',at:'2026-11-13T00:30:00Z'};info.tasks[0].next_run='2026-11-14T10:15:00Z';info.tasks[1].next_run='2026-11-15T11:45:00Z';info.tasks[2].date='2026-12-01';info.task_error='';await f.locator('#refresh-tasks').click();await f.locator('#task-service-error').waitFor({state:'hidden'});await page.screenshot({path:path.join(repo,'docs/assets/tasks-desktop.png')});
  await f.locator('#add-task').click();await f.locator('#task-repeat').selectOption('daily');mode='failure';await f.locator('#save-task').click();await f.locator('#task-form-error').filter({hasText:'任务保存失败'}).waitFor();assert(await f.locator('#save-task').isEnabled());mode='normal';await f.locator('#cancel-task').click();await f.locator('#add-task').click();await f.locator('#task-repeat').selectOption('custom');checks.push('save failures preserve the editable task form');
  for(const width of [1440,1024,768,390,320]){
   await page.setViewportSize({width,height:900});const sizes=await page.frames()[1].evaluate(()=>{const d=document.getElementById('task-dialog');return {page:document.documentElement.scrollWidth<=innerWidth,dialog:d.scrollWidth<=d.clientWidth,height:d.getBoundingClientRect().height<=innerHeight-20};});assert(sizes.page&&sizes.dialog&&sizes.height,`layout ${width}: ${JSON.stringify(sizes)}`);
   if(width===390){await page.setViewportSize({width:390,height:1100});await f.locator('#task-dialog').screenshot({path:path.join(repo,'docs/assets/tasks-form-mobile.png'),animations:'disabled'});}
  }await f.locator('#cancel-task').click();await page.setViewportSize({width:390,height:900});await page.screenshot({path:path.join(repo,'docs/assets/tasks-mobile.png')});checks.push('task cards, four mobile nav items and custom-day form fit down to 320px');
  await f.locator('.task-delete').first().click();await f.locator('#delete-task-dialog button[value=cancel]').click();assert.equal(info.tasks.length,3);await f.locator('.task-delete').first().click();await f.locator('#delete-task-dialog button[value=confirm]').click();await f.locator('.task-card').nth(2).waitFor({state:'hidden'});assert.equal(info.tasks.length,2);checks.push('task deletion supports cancel and confirm');
  info.tasks=[];info.devices=[];await f.locator('#refresh-tasks').click();await f.locator('#tasks-empty-description').filter({hasText:'先添加'}).waitFor();await f.locator('#add-first-task').click();await f.locator('#device-dialog').waitFor({state:'visible'});await f.locator('#cancel-device').click();checks.push('no-device task creation routes to adding a device');
  info.devices=[{id:'pc',name:'电脑',mac:'02:11:22:33:44:55',network:'auto'}];await f.locator('#refresh').click();await f.locator('[data-view=tasks]').click();await f.locator('#add-task').click();mode='denied';await f.locator('#save-task').click();await f.locator('#session-title').filter({hasText:'无权访问'}).waitFor();assert(await f.locator('#task-dialog').isHidden());checks.push('authorization failure closes the task dialog');
  assert.equal(wakes,0);assert.deepEqual(errors,[]);const report={checks,pageErrors:errors,validation:'Isolated Chrome with mocked UGOS SDK/API; no LAN or WOL packets sent.'};fs.writeFileSync(path.join(repo,'docs/TASKS-UI-VALIDATION.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));
 }finally{await browser.close();await new Promise(r=>server.close(r));fs.rmSync(temp,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1});
