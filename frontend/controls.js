// Shared UGOS-style controls. Native select values remain the form data source.
const $ = id => document.getElementById(id);
const selects = new Map();
let makeIcon, popup, trigger, serial = 0;
const cityNames = {'Asia/Shanghai':'北京、上海','Asia/Hong_Kong':'香港','Asia/Tokyo':'东京','Asia/Singapore':'新加坡','UTC':'协调世界时','Europe/London':'伦敦','Europe/Berlin':'柏林','Europe/Paris':'巴黎','America/New_York':'纽约','America/Los_Angeles':'洛杉矶'};
export function zoneLabel(zone) {
  try {
    const offset = new Intl.DateTimeFormat('en',{timeZone:zone,timeZoneName:'longOffset'}).formatToParts(new Date()).find(p=>p.type==='timeZoneName').value.replace('GMT','UTC');
    return `(${offset === 'UTC' ? 'UTC+00:00' : offset}) ${cityNames[zone] || zone}`;
  } catch { return zone; }
}
const node = (tag,content,cls='') => { const n=document.createElement(tag);if(content !== undefined)n.textContent=content;n.className=cls;return n; };
export function closeControls(focus=false) {
  popup?.remove();popup=null;
  if(trigger){trigger.classList.remove('control-open');trigger.setAttribute('aria-expanded','false');if(focus&&trigger.isConnected)trigger.focus();}
  trigger=null;
}
function positionPopup(p,anchor,width) {
  const r=anchor.getBoundingClientRect(),edge=12;
  p.style.width=`${Math.min(width||r.width,innerWidth-edge*2)}px`;
  p.style.maxHeight=`${Math.max(120,innerHeight-edge*2)}px`;
  const h=p.getBoundingClientRect().height,below=innerHeight-r.bottom-edge,above=r.top-edge;
  const top=below<h&&above>below?r.top-h-8:r.bottom+8;
  p.style.left=`${Math.max(edge,Math.min(r.left,innerWidth-p.getBoundingClientRect().width-edge))}px`;
  p.style.top=`${Math.max(edge,Math.min(top,innerHeight-h-edge))}px`;
}
function openPopup(button,cls,anchor=button,width) {
  closeControls();trigger=button;button.classList.add('control-open');button.setAttribute('aria-expanded','true');
  const p=node('div',undefined,`control-popup ${cls}`);p.id=`control-popup-${++serial}`;
  button.setAttribute('aria-controls',p.id);
  // Stay inside the active dialog's top layer, including embedded UGOS windows.
  (button.closest('dialog') || document.body).append(p);popup=p;
  p.addEventListener('keydown',e=>{if(e.key==='Escape'){e.preventDefault();e.stopPropagation();closeControls(true);}});
  p.place=()=>positionPopup(p,anchor,width);
  return p;
}
function selectIcon(select,value) {
  return select.id==='device-os' ? ({windows:'os_windows',macos:'os_apple',linux:'os_linux'}[value]||'desktop') : '';
}
function syncSelect(select) {
  const widget=selects.get(select);if(!widget)return;
  const option=select.selectedOptions[0];widget.button.replaceChildren();
  const glyph=selectIcon(select,select.value);if(glyph)widget.button.append(makeIcon(glyph));
  widget.button.append(node('span',option?.textContent||'请选择','control-value'),makeIcon('chevron_down'));
  widget.button.disabled=select.disabled;widget.button.title=option?.textContent||'';
}
function enhanceSelect(select) {
  const wrap=node('div',undefined,'select-control'),button=node('button',undefined,'select-trigger');
  select.before(wrap);wrap.append(select,button);select.classList.add('control-native');
  select.tabIndex=-1;select.setAttribute('aria-hidden','true');
  button.type='button';button.id=`${select.id}-control`;button.setAttribute('aria-haspopup','listbox');button.setAttribute('aria-expanded','false');
  const label=document.querySelector(`label[for="${select.id}"]`);
  if(label){label.htmlFor=button.id;label.id ||= `${select.id}-label`;button.setAttribute('aria-labelledby',label.id);}
  else button.setAttribute('aria-label',select.getAttribute('aria-label')||'选择');
  if(select.getAttribute('aria-describedby'))button.setAttribute('aria-describedby',select.getAttribute('aria-describedby'));
  selects.set(select,{button});
  function showMenu() {
    if(trigger===button){closeControls();return;}
    const p=openPopup(button,'select-popup');p.setAttribute('role','listbox');p.setAttribute('aria-label',label?.textContent||'选择');
    let search;
    if(select.id==='task-timezone'){
      const box=node('div',undefined,'control-search');search=node('input');search.type='search';search.placeholder='搜索城市或时区';search.setAttribute('aria-label','搜索时区');box.append(makeIcon('search'),search);p.append(box);
    }
    const list=node('div',undefined,'control-options');p.append(list);
    function render(term='') {
      list.replaceChildren();
      const options=[...select.options].filter(o=>(o.textContent+' '+o.value).toLowerCase().includes(term.toLowerCase()));
      for(const option of options){
        const item=node('button',undefined,'control-option');item.type='button';item.setAttribute('role','option');item.disabled=option.disabled;item.setAttribute('aria-selected',String(option.selected));item.title=option.textContent;
        const glyph=selectIcon(select,option.value);if(glyph)item.append(makeIcon(glyph));item.append(node('span',option.textContent,'control-value'));if(option.selected)item.append(makeIcon('checkmark'));
        item.addEventListener('click',()=>{select.value=option.value;select.dispatchEvent(new Event('input',{bubbles:true}));select.dispatchEvent(new Event('change',{bubbles:true}));syncSelect(select);closeControls(true);});list.append(item);
      }
      if(!options.length)list.append(node('p','没有匹配结果','control-empty'));
      p.place();
    }
    render();
    if(search){search.addEventListener('input',()=>render(search.value));search.focus();}
    else {const selected=list.querySelector('[aria-selected=true]')||list.querySelector('button:not(:disabled)');selected?.focus({preventScroll:true});if(selected)list.scrollTop=Math.max(0,selected.offsetTop-list.offsetTop-list.clientHeight/2);}
    p.addEventListener('keydown',e=>{
      const options=[...list.querySelectorAll('button:not(:disabled)')];if(!options.length)return;
      if(!['ArrowDown','ArrowUp','Home','End'].includes(e.key)||e.target===search&&['Home','End'].includes(e.key))return;
      e.preventDefault();let i=options.indexOf(document.activeElement);
      i=e.key==='Home'?0:e.key==='End'?options.length-1:(i+(e.key==='ArrowDown'?1:-1)+options.length)%options.length;
      options[i].focus();
    });
  }
  button.addEventListener('click',showMenu);
  button.addEventListener('keydown',e=>{if(['ArrowDown','ArrowUp'].includes(e.key)){e.preventDefault();showMenu();}});
  select.addEventListener('change',()=>syncSelect(select));
  select.addEventListener('invalid',e=>{e.preventDefault();button.focus();});
  new MutationObserver(()=>{if(trigger===button)closeControls();syncSelect(select);}).observe(select,{childList:true,subtree:true,attributes:true,attributeFilter:['disabled','label','selected']});
  syncSelect(select);
}
export function syncControls() { for(const select of selects.keys())syncSelect(select);updateZoneHelp(); }
function parseTime(value) {const m=value.trim().match(/^(\d{1,2}):(\d{2})$/);return m&&+m[1]<24&&+m[2]<60?[+m[1],+m[2]]:null;}
export function normalizeClock() {
  const input=$('task-time'),parts=parseTime(input.value),error=parts?'':'请输入有效时间，例如 08:30（00:00–23:59）。';
  input.setCustomValidity(error);input.setAttribute('aria-invalid',String(!parts));$('task-clock-error').textContent=error;
  if(parts)input.value=parts.map(n=>String(n).padStart(2,'0')).join(':');
  return !!parts;
}
function parseDate(value) {
  const m=value.match(/^(\d{4})-(\d{2})-(\d{2})$/);if(!m||+m[1]<1||+m[1]>9999)return null;
  const y=+m[1],month=+m[2],day=+m[3],d=new Date(0);d.setFullYear(y,month-1,day);d.setHours(12,0,0,0);
  return d.getFullYear()===y&&d.getMonth()===month-1&&d.getDate()===day?[y,month-1,day]:null;
}
export function validateTaskDate() {
  const input=$('task-date');const valid=input.disabled||!!parseDate(input.value);
  input.setCustomValidity(valid?'':'请输入有效日期，格式为 YYYY-MM-DD。');input.setAttribute('aria-invalid',String(!valid));return valid;
}
function updateZoneHelp() {const zone=$('task-timezone').value;$('task-zone-help').textContent=zone?zoneLabel(zone):'';}
function timePicker() {
  const button=$('task-time-trigger');if(trigger===button){closeControls();return;}
  let [hour,minute]=parseTime($('task-time').value)||[8,30];
  const p=openPopup(button,'clock-popup',$('task-time').closest('.picker-field'),280);p.setAttribute('role','dialog');p.setAttribute('aria-label','选择时间');
  const columns=node('div',undefined,'clock-columns'),lists=[];
  for(const name of ['小时','分钟']){const list=node('div',undefined,'clock-column');list.setAttribute('role','listbox');list.setAttribute('aria-label',name);columns.append(list);lists.push(list);}p.append(columns);
  function render() {
    lists.forEach((list,part)=>{list.replaceChildren();const count=part?60:24,value=part?minute:hour;
      for(let n=0;n<count;n++){const b=node('button',String(n).padStart(2,'0'),'clock-option');b.type='button';b.dataset.number=n;b.setAttribute('role','option');b.setAttribute('aria-selected',String(n===value));b.addEventListener('click',()=>{if(part)minute=n;else hour=n;for(const other of list.children)other.setAttribute('aria-selected',String(other===b));});list.append(b);}
      list.scrollTop=Math.max(0,value*42-84);
    });
  }
  const actions=node('div',undefined,'clock-actions'),now=node('button','此刻','secondary'),confirm=node('button','确定','primary');now.type=confirm.type='button';actions.append(now,confirm);p.append(actions);p.place();render();
  now.addEventListener('click',()=>{const parts=new Intl.DateTimeFormat('en-GB',{timeZone:$('task-timezone').value,hour:'2-digit',minute:'2-digit',hourCycle:'h23'}).formatToParts(new Date());hour=+parts.find(p=>p.type==='hour').value;minute=+parts.find(p=>p.type==='minute').value;render();});
  confirm.addEventListener('click',()=>{$('task-time').value=[hour,minute].map(n=>String(n).padStart(2,'0')).join(':');normalizeClock();closeControls(true);});
  lists.forEach(list=>list.addEventListener('keydown',e=>{if(!['ArrowDown','ArrowUp','Home','End'].includes(e.key))return;e.preventDefault();const all=[...list.children],i=all.indexOf(document.activeElement);all[e.key==='Home'?0:e.key==='End'?all.length-1:(i+(e.key==='ArrowDown'?1:-1)+all.length)%all.length].focus();}));
  (lists[0].querySelector('[aria-selected=true]')||confirm).focus({preventScroll:true});
}
function datePicker() {
  const button=$('task-date-trigger');if(trigger===button){closeControls();return;}
  let [year,month]=parseDate($('task-date').value)||[new Date().getFullYear(),new Date().getMonth()];
  const p=openPopup(button,'date-popup',$('task-date').closest('.picker-field'),300);p.setAttribute('role','dialog');p.setAttribute('aria-label','选择日期');
  const dateValue=(y,m,d)=>`${String(y).padStart(4,'0')}-${String(m+1).padStart(2,'0')}-${String(d).padStart(2,'0')}`;
  function render() {
    p.replaceChildren();const head=node('div',undefined,'date-head'),prev=node('button'),next=node('button');prev.type=next.type='button';prev.append(makeIcon('chevron_left'));next.append(makeIcon('chevron_right'));prev.setAttribute('aria-label','上个月');next.setAttribute('aria-label','下个月');prev.disabled=year===1&&month===0;next.disabled=year===9999&&month===11;head.append(prev,node('strong',`${year} 年 ${month+1} 月`),next);p.append(head);
    const grid=node('div',undefined,'date-grid');for(const d of ['一','二','三','四','五','六','日'])grid.append(node('span',d));
    const first=new Date(0);first.setFullYear(year,month,1);first.setHours(12,0,0,0);const end=new Date(first);end.setMonth(month+1,0);
    for(let n=0;n<(first.getDay()+6)%7;n++)grid.append(node('span',''));
    for(let day=1;day<=end.getDate();day++){const value=dateValue(year,month,day),b=node('button',String(day));b.type='button';b.setAttribute('aria-label',value);b.setAttribute('aria-pressed',String($('task-date').value===value));b.addEventListener('click',()=>{$('task-date').value=value;validateTaskDate();closeControls(true);});grid.append(b);}p.append(grid);p.place();
    prev.addEventListener('click',()=>{if(--month<0){month=11;year--;}render();});next.addEventListener('click',()=>{if(++month>11){month=0;year++;}render();});
  }
  render();(p.querySelector('[aria-pressed=true]')||p.querySelector('.date-grid button')).focus({preventScroll:true});
}
export function initControls(icon) {
  makeIcon=icon;document.querySelectorAll('select').forEach(enhanceSelect);
  document.addEventListener('pointerdown',e=>{if(popup&&!popup.contains(e.target)&&!trigger?.contains(e.target))closeControls();},true);
  document.addEventListener('keydown',e=>{if(popup&&e.key==='Escape'){e.preventDefault();e.stopImmediatePropagation();closeControls(true);}else if(popup&&e.key==='Tab'){const previous=trigger;closeControls();previous?.focus();}},true);
  document.addEventListener('scroll',e=>{if(popup&&!popup.contains(e.target))closeControls();},true);window.addEventListener('resize',()=>closeControls());
  document.querySelectorAll('dialog').forEach(d=>{d.addEventListener('close',()=>closeControls());d.addEventListener('cancel',e=>{if(popup){e.preventDefault();closeControls(true);}});});
  $('task-time-trigger').addEventListener('click',timePicker);$('task-date-trigger').addEventListener('click',datePicker);
  $('task-time').addEventListener('input',()=>{$('task-time').setCustomValidity('');$('task-time').removeAttribute('aria-invalid');$('task-clock-error').textContent='';});
  $('task-time').addEventListener('blur',normalizeClock);$('task-time').addEventListener('keydown',e=>{if(e.key==='Enter'){e.preventDefault();normalizeClock();}});
  $('task-date').addEventListener('input',()=>{$('task-date').setCustomValidity('');$('task-date').removeAttribute('aria-invalid');});$('task-date').addEventListener('blur',validateTaskDate);
  $('task-timezone').addEventListener('change',updateZoneHelp);
  $('task-more').addEventListener('click',()=>{const open=$('task-advanced').hidden;$('task-advanced').hidden=!open;$('task-more').setAttribute('aria-expanded',String(open));closeControls();});
}
