// Optional browser regression: set PLAYWRIGHT_MODULE and BROWSER_EXECUTABLE
// when using an externally installed Playwright/browser runtime.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('node:fs'),http=require('node:http'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const repo=path.resolve(__dirname,'../..'),root=path.join(repo,'backend/web');
 const sprite=fs.readFileSync(path.join(root,'icons.svg'),'utf8');
 const legacy=sprite.replace(/<symbol id="iot-20"[\s\S]*?<\/symbol>/,'');
 assert(!legacy.includes('id="iot-20"'));
 const html=fs.readFileSync(path.join(root,'index.html'),'utf8').replace(/<script[^>]*>[\s\S]*?<\/script>/g,'').replace('id="app" class="shell" hidden','id="app" class="shell"').replace('id="session-screen"','hidden id="session-screen"');
 const initial=html.replace(/(icons|icon|style|app)\.[0-9a-f]{12}\.(svg|css|js)/g,'$1.$2'),broken=initial.replace('icons.svg#desktop-20','icons.svg#iot-20');
 const requests=[];
 let upgraded=false;
 const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://local');
  if(['/initial','/broken','/fixed'].includes(url.pathname)){
   res.setHeader('Content-Type','text/html');res.setHeader('Cache-Control','no-store');
   res.end(url.pathname==='/initial'?initial:url.pathname==='/broken'?broken:html);return;
  }
  if(url.pathname==='/icons.svg'||/^\/icons\.[0-9a-f]{12}\.svg$/.test(url.pathname)){
   requests.push(req.url);res.setHeader('Content-Type','image/svg+xml');res.setHeader('Cache-Control','public,max-age=86400');res.end(upgraded?sprite:legacy);return;
  }
  if(['/style.css','/icon.svg'].includes(url.pathname)||/^\/(style|icon)\.[0-9a-f]{12}\.(css|svg)$/.test(url.pathname)){
   res.setHeader('Content-Type',url.pathname.endsWith('.css')?'text/css':'image/svg+xml');res.setHeader('Cache-Control','public,max-age=86400');res.end(fs.readFileSync(path.join(root,url.pathname.slice(1).replace(/^(style|icon)\.[0-9a-f]{12}\.(css|svg)$/,'$1.$2'))));return;
  }
  res.writeHead(404);res.end();
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const options={headless:true};if(process.env.BROWSER_EXECUTABLE)options.executablePath=process.env.BROWSER_EXECUTABLE;
 const browser=await chromium.launch(options);
 try{
  const p=await browser.newPage({viewport:{width:1280,height:800},deviceScaleFactor:2});
  const base=`http://127.0.0.1:${server.address().port}`;
  const boxes=()=>p.locator('.nav use').evaluateAll(nodes=>nodes.map(n=>({width:n.getBBox().width,height:n.getBBox().height})));
  await p.goto(base+'/initial');await p.waitForLoadState('networkidle');const before=await boxes();
  upgraded=true;await p.goto(base+'/broken');await p.waitForLoadState('networkidle');const reproduced=await boxes();
  assert(before.every(b=>b.width>0));assert.equal(reproduced[0].width,0);assert(reproduced[1].width>0&&reproduced[2].width>0);
  assert.equal(requests.length,1,'the old URL must be served from browser cache');
  await p.goto(base+'/fixed');await p.waitForLoadState('networkidle');const fixed=await boxes();
  assert(fixed.every(b=>b.width>0));assert.equal(requests.length,2);assert(/^\/icons\.[0-9a-f]{12}\.svg$/.test(requests[1]));
  const positions=await p.locator('.nav').evaluateAll(nodes=>nodes.map(n=>({iconX:n.querySelector('svg').getBoundingClientRect().x,textX:n.querySelector('span').getBoundingClientRect().x})));
  assert(positions.every(p=>p.iconX===positions[0].iconX&&p.textX===positions[0].textX));
  await p.screenshot({path:path.join(repo,'docs/assets/sidebar-restored.png'),clip:{x:0,y:0,width:244,height:350}});
  const report={checks:['cached old sprite reproduces missing device icon','content fingerprint bypasses old cache without changing app version','all three navigation icons render and align'],before,reproduced,fixed,requests};
  fs.writeFileSync(path.join(repo,'docs/ASSET-CACHE-VALIDATION.json'),JSON.stringify(report,null,2)+'\n');
  console.log(JSON.stringify(report,null,2));
 }finally{await browser.close();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);process.exitCode=1});
