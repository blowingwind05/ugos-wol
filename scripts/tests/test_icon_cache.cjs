// Regression for embedded browsers that cannot render external SVG fragments.
const {chromium}=require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const fs=require('node:fs'),http=require('node:http'),path=require('node:path'),assert=require('node:assert/strict');
(async()=>{
 const repo=path.resolve(__dirname,'../..'),root=path.join(repo,'backend/web');
 const reveal=html=>html.replace(/<script[^>]*>[\s\S]*?<\/script>/g,'').replace('id="app" class="shell" hidden','id="app" class="shell"').replace('id="session-screen"','hidden id="session-screen"');
 const legacy=reveal(fs.readFileSync(path.join(repo,'frontend/index.html'),'utf8'));
 const fixed=reveal(fs.readFileSync(path.join(root,'index.html'),'utf8'));
 const blocked=[];
 const server=http.createServer((req,res)=>{
  const url=new URL(req.url,'http://local');
  if(url.pathname==='/legacy'||url.pathname==='/fixed'){res.setHeader('Content-Type','text/html');res.end(url.pathname==='/legacy'?legacy:fixed);return;}
  if(/^\/icons(?:\.[0-9a-f]{12})?\.svg$/.test(url.pathname)){blocked.push(url.pathname);res.writeHead(403);res.end('SVG sprite blocked for regression');return;}
  const file=path.join(root,url.pathname.slice(1));
  if(!file.startsWith(root+path.sep)){res.writeHead(403);res.end();return;}
  try{res.setHeader('Content-Type',file.endsWith('.css')?'text/css':file.endsWith('.svg')?'image/svg+xml':'text/html');res.end(fs.readFileSync(file));}catch{res.writeHead(404);res.end();}
 });
 await new Promise(r=>server.listen(0,'127.0.0.1',r));
 const options={headless:true};if(process.env.BROWSER_EXECUTABLE)options.executablePath=process.env.BROWSER_EXECUTABLE;
 const browser=await chromium.launch(options);
 try{
  const page=await browser.newPage({viewport:{width:1280,height:850},deviceScaleFactor:1});
  const base=`http://127.0.0.1:${server.address().port}`;
  const boxes=()=>page.locator('.nav svg').evaluateAll(nodes=>nodes.map(n=>({width:n.getBBox().width,height:n.getBBox().height})));
  await page.goto(base+'/legacy');await page.waitForLoadState('networkidle');const before=await boxes();assert.equal(before.length,4);assert(before.every(b=>b.width===0));assert(blocked.length>0);
  const requestsBefore=blocked.length;
  await page.goto(base+'/fixed');await page.waitForLoadState('networkidle');const after=await boxes();assert(after.every(b=>b.width>0&&b.height>0));assert.equal(blocked.length,requestsBefore);assert.equal(await page.locator('use').count(),0);
  const positions=await page.locator('.nav').evaluateAll(nodes=>nodes.map(n=>({iconX:n.querySelector('svg').getBoundingClientRect().x,textX:n.querySelector('span').getBoundingClientRect().x})));
  assert(positions.every(p=>p.iconX===positions[0].iconX&&p.textX===positions[0].textX));
  await page.screenshot({path:path.join(repo,'docs/assets/inline-icons.png'),clip:{x:0,y:0,width:244,height:380}});
  const report={checks:['blocked external SVG reproduces all four missing navigation icons','inline SVG paths render with sprite requests still blocked','fixed page makes no external sprite requests and contains no SVG use elements','all four icon slots and labels align'],before,after,blockedRequests:blocked,positions,scope:'Simulated external-resource failure in isolated Chrome; does not identify the exact restriction in the user client.'};
  fs.writeFileSync(path.join(repo,'docs/INLINE-ICONS-VALIDATION.json'),JSON.stringify(report,null,2)+'\n');console.log(JSON.stringify(report,null,2));
 }finally{await browser.close();await new Promise(r=>server.close(r));}
})().catch(e=>{console.error(e);process.exitCode=1});
