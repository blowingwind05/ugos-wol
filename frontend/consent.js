export const POLICY_VERSION='2026-11-17';
const KEY='ugos-wol-consent';let memory=false;const $=id=>document.getElementById(id);
export function hasConsent(){if(memory)return true;try{const s=JSON.parse(localStorage.getItem(KEY)||'null');return s?.version===POLICY_VERSION&&s?.accepted===true;}catch{return false;}}
export function showConsent(){$('consent-check').checked=false;$('consent-accept').disabled=true;$('consent-decline').textContent=hasConsent()?'撤回本端同意':'不同意';if(!$('consent-dialog').open)$('consent-dialog').showModal();}
export function initConsent(accept,decline){
 $('consent-check').addEventListener('change',()=>$('consent-accept').disabled=!$('consent-check').checked);
 $('consent-accept').addEventListener('click',()=>{if(!$('consent-check').checked)return;memory=true;try{localStorage.setItem(KEY,JSON.stringify({version:POLICY_VERSION,accepted:true,accepted_at:new Date().toISOString()}));}catch{}$('consent-dialog').close();accept();});
 const reject=()=>{memory=false;try{localStorage.removeItem(KEY);}catch{}$('consent-dialog').close();decline();};
 $('consent-decline').addEventListener('click',reject);$('consent-dialog').addEventListener('cancel',e=>{e.preventDefault();reject();});document.querySelectorAll('[data-open-consent]').forEach(b=>b.addEventListener('click',showConsent));
}
