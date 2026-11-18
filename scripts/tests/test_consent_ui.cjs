// Development-only SDK/API fixtures; production authentication is unchanged.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('node:fs'),http=require('node:http'),path=require('node:path'),os=require('node:os'),assert=require('node:assert/strict');
const repo=path.resolve(__dirname,'../..');
const {build}=require(path.join(repo,'frontend/node_modules/esbuild'));
(async()=>{
 const root=path.join(repo,'backend/web'),temp=fs.mkdtempSync(path.join(os.tmpdir(),'ugos-wol-tasks-ui-')),bundle=path.join(temp,'app.js');
 const html=fs.readFileSync(path.join(root,'index.html'),'utf8');
 await build({entryPoints:[path.join(repo,'frontend/app.js')],outfile:bundle,bundle:true,format:'esm',plugins:[{name:'test-sdk',setup(b){b.onResolve({filter:/^@ugreen-nas\/core/},args=>({path:args.path,namespace:'fixture'}));b.onLoad({filter:/.*/,namespace:'fixture'},args=>({contents:args.path.endsWith('cloudWindow')?'export default {useCapacity:async()=>{window.tokenCalls=(window.tokenCalls||0)+1;if(window.delayToken)await new Promise(r=>window.releaseToken=r);return {third_token:"fixture-token"}}}':'export default {init:async()=>{window.sdkInitializations=(window.sdkInitializations||0)+1}}',loader:'js'}));}}]});
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
  const info={version:'0.5.2',devices:[{id:'pc',name:'办公电脑',mac:'02:11:22:33:44:55',network:'auto'}],tasks:[],history:[],networks:[network],default_timezone:'Asia/Shanghai',server_time:new Date().toISOString()};
  let posted,mode='normal',serial=0,wakes=0,apiRequests=0;
  const inputFor=body=>({...body,name:body.name||'办公电脑定时唤醒',date:body.repeat==='once'?body.date:undefined,weekdays:{daily:[1,2,3,4,5,6,7],weekdays:[1,2,3,4,5],weekends:[6,7]}[body.repeat]||body.weekdays});
  await page.route('**/api/wol/v1/**',async route=>{
   apiRequests++;const req=route.request(),key=new URL(req.url()).pathname.split('/v1/')[1];assert.equal(req.headers()['ugreen-ttk'],'fixture-token');
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
  await page.goto(`http://127.0.0.1:${server.address().port}/host`);const f=page.frameLocator('iframe');await f.locator('#consent-dialog').waitFor({state:'visible'});


  assert.equal(apiRequests,0);assert.equal(await page.frames()[1].evaluate(()=>window.sdkInitializations||0),0);assert(await f.locator('#consent-accept').isDisabled());
  await page.keyboard.press('Escape');assert(await f.locator('#app').isHidden());assert.equal(apiRequests,0);assert.equal(await page.frames()[1].evaluate(()=>localStorage.getItem('ugos-wol-consent')),null);await f.locator('#retry-session').click();await f.locator('#consent-dialog').waitFor({state:'visible'});checks.push('Escape before first agreement does not grant consent or access APIs');
  await f.locator('#consent-decline').click();assert.equal(apiRequests,0);assert(await f.locator('#app').isHidden());await f.locator('#retry-session').click();await f.locator('#consent-dialog').waitFor({state:'visible'});checks.push('No SDK initialization, token acquisition or API requests before explicit agreement, including refusal');
  await f.locator('#consent-check').check();await f.locator('#consent-accept').click();await f.locator('#app').waitFor({state:'visible'});assert.equal(await page.frames()[1].evaluate(()=>window.sdkInitializations),1);assert(apiRequests>0);checks.push('Unchecked consent is required; agreement initializes SDK and enters existing login flow');
  const frame=page.frames()[1];
  const accepted=await frame.evaluate(()=>localStorage.getItem('ugos-wol-consent'));
  const callsBeforeReview={api:apiRequests,sdk:await frame.evaluate(()=>window.sdkInitializations),token:await frame.evaluate(()=>window.tokenCalls)};
  for(const close of ['button','escape','title']){
   await f.locator('[data-open-consent]').click();
   assert(await f.locator('#consent-check').isHidden());assert(await f.locator('#consent-close').isVisible());assert.equal(await f.locator('#consent-accept').textContent(),'关闭');assert(await f.locator('#consent-accept').isEnabled());
   if(close==='button')await f.locator('#consent-accept').click();else if(close==='title')await f.locator('#consent-close').click();else await page.keyboard.press('Escape');
   assert(await f.locator('#consent-dialog').isHidden());assert(await f.locator('#app').isVisible());assert.equal(await frame.evaluate(()=>localStorage.getItem('ugos-wol-consent')),accepted);
  }
  assert.equal(apiRequests,callsBeforeReview.api);assert.equal(await frame.evaluate(()=>window.sdkInitializations),callsBeforeReview.sdk);assert.equal(await frame.evaluate(()=>window.tokenCalls),callsBeforeReview.token);
  checks.push('Review closes by button, Escape or title control without repeated consent, changed acceptance time or new SDK/API activity');
  const reviewPage=await browser.newPage({viewport:{width:390,height:820}});
  await reviewPage.route('**/api/wol/v1/**',route=>route.fulfill({json:route.request().url().endsWith('/info')?info:{devices:[]}}));
  await reviewPage.addInitScript(value=>localStorage.setItem('ugos-wol-consent',value),accepted);
  await reviewPage.goto(`http://127.0.0.1:${server.address().port}/host`);const rf=reviewPage.frameLocator('iframe');await rf.locator('#app').waitFor({state:'visible'});assert(await rf.locator('#consent-dialog').isHidden());
  await rf.locator('[data-open-consent]').click();assert(await reviewPage.frames()[1].evaluate(()=>['consent-accept','consent-decline','consent-close'].every(id=>{const r=document.getElementById(id).getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&r.top>=0&&r.bottom<=innerHeight})));await reviewPage.screenshot({path:path.join(repo,'docs/assets/consent-review-mobile.png')});await reviewPage.keyboard.press('Escape');assert(await rf.locator('#app').isVisible());await reviewPage.close();
  checks.push('A stored current agreement opens a new session without a consent prompt, including mobile review and Escape');
  for(const file of ['about.html','privacy.html','agreement.html','permissions.html','help.html','support.html']){const r=await frame.evaluate(async path=>{const r=await fetch(path);return {status:r.status,html:await r.text()};},file);assert.equal(r.status,200);assert(r.html.includes('blowingwind'));}checks.push('Policy, license, permission details, help and support are available as local static pages');
  await f.locator('[data-open-consent]').click();await f.locator('#consent-decline').click();assert(await f.locator('#app').isHidden());assert.equal(await frame.evaluate(()=>localStorage.getItem('ugos-wol-consent')),null);const stopped=apiRequests;await page.waitForTimeout(1200);assert.equal(apiRequests,stopped);checks.push('Withdrawal clears local consent and stops subsequent frontend API access');
  // A token obtained after withdrawal must never be used for a new backend request.
  await frame.evaluate(()=>{window.delayToken=true;});
  await f.locator('#retry-session').click();await f.locator('#consent-check').check();await f.locator('#consent-accept').click();
  await page.waitForFunction(()=>typeof document.querySelector('iframe').contentWindow.releaseToken==='function');
  await frame.evaluate(()=>document.getElementById('consent-dialog').showModal());await f.locator('#consent-decline').click();
  const beforeLateToken=apiRequests;await frame.evaluate(()=>window.releaseToken());await page.waitForTimeout(150);assert.equal(apiRequests,beforeLateToken);assert(await f.locator('#app').isHidden());
  await frame.evaluate(()=>{window.delayToken=false;});checks.push('A delayed SDK token returned after withdrawal cannot start an API request');
  // Confirm the compatibility error is exportable and excludes user values/tokens.
  await page.route('**/api/wol/v1/info',r=>r.fulfill({status:401,json:{error:'无法确认NAS登录身份',code:'UGOS_IDENTITY_UNAVAILABLE',diagnostic:{id_header_count:0,name_header_count:1}}}));
  await f.locator('#retry-session').click();await f.locator('#consent-check').check();await f.locator('#consent-accept').click();await f.locator('#session-message').filter({hasText:'无法确认NAS登录身份'}).waitFor();await f.locator('#download-diagnostic').waitFor({state:'visible'});
  const [download]=await Promise.all([page.waitForEvent('download'),f.locator('#download-diagnostic').click()]);const diagnostic=JSON.parse(fs.readFileSync(await download.path(),'utf8'));assert.equal(diagnostic.code,'UGOS_IDENTITY_UNAVAILABLE');assert.equal(diagnostic.details.id_header_count,0);assert(!JSON.stringify(diagnostic).includes('fixture-token'));checks.push('Firmware compatibility errors can be downloaded as a diagnostic JSON without tokens');
  await frame.evaluate(()=>document.getElementById('consent-dialog').showModal());await f.locator('#consent-decline').click();await page.setViewportSize({width:320,height:700});await f.locator('#retry-session').click();assert(await frame.evaluate(()=>{const e=document.getElementById('consent-dialog'),r=e.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth&&e.scrollWidth<=e.clientWidth}));await page.screenshot({path:path.join(repo,'docs/assets/consent-mobile.png')});checks.push('Consent and legal links fit a 320px mobile viewport');
  const outdated=await browser.newPage();let outdatedRequests=0;
  await outdated.addInitScript(()=>localStorage.setItem('ugos-wol-consent',JSON.stringify({version:'old-policy',accepted:true})));
  await outdated.route('**/api/wol/v1/**',route=>{outdatedRequests++;return route.fulfill({json:info});});
  await outdated.goto(`http://127.0.0.1:${server.address().port}/host`);const of=outdated.frameLocator('iframe');await of.locator('#consent-dialog').waitFor({state:'visible'});assert(await of.locator('#consent-check').isVisible());assert(await of.locator('#consent-accept').isDisabled());assert.equal(outdatedRequests,0);assert.equal(await outdated.frames()[1].evaluate(()=>window.sdkInitializations||0),0);await outdated.close();checks.push('An outdated policy version still requires explicit unchecked agreement before SDK/API use');
  assert.equal(wakes,0);assert.deepEqual(errors,[]);const report={checks,pageErrors:errors,validation:'Actual frontend with mocked UGOS SDK/API; no LAN or WOL packets.'};fs.writeFileSync(path.join(repo,'docs/CONSENT-UI-VALIDATION.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));
 }finally{await browser.close();await new Promise(r=>server.close(r));fs.rmSync(temp,{recursive:true,force:true});}
})().catch(e=>{console.error(e);process.exitCode=1});
