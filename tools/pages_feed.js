/* GitHub Pages data bridge. GitHub Actions refreshes the published JSON.
   All portfolio calculations are performed in this browser, not in GitHub Actions. */
(function(){
'use strict';
const researchDate=D.asOf;
const live={status:{message:'Checking the most recent published data',errors:{}},data:null,revision:null,polling:false,error:'',lastCheck:null};
window.AutoFeed=live;
const originalRender=render, originalSources=sources, originalCatalysts=catalysts;
function liveMarks(){
 if(!live.data)return {covered:0,total:0,rows:[],oldest:null,stale:true};
 const rows=ORIGINAL.map(h=>{const m=live.data.marks?.[h.ticker];const age=m?(Date.now()-Date.parse(m.date+'T00:00:00Z'))/86400000:Infinity;return {h,m,age,value:m?h.quantity*m.gbp:NaN}});
 return {covered:rows.filter(r=>r.m&&Number.isFinite(r.value)).length,total:rows.reduce((s,r)=>s+(Number.isFinite(r.value)?r.value:0),0),rows,oldest:rows.filter(r=>r.m).map(r=>r.m.date).sort()[0],stale:rows.some(r=>r.age>7||live.data.errors?.[r.h.ticker])};
}
function readableDate(value){try{return value?new Date(value).toLocaleString('en-GB',{dateStyle:'medium',timeStyle:'short'}):'Not yet published'}catch(_){return value||'Not yet published'}}
function health(){
 const s=live.status,covered=ORIGINAL.filter(h=>state.history[h.ticker]?.length).length,m=liveMarks(),errors=Object.entries(live.data?.errors||{});
 return card('Automatic GitHub Pages market data','GitHub Actions downloads the public market data on a schedule and republishes this website. You never need to import a file or keep a desktop application running.',
 `<div class="grid g3">${metric('HOLDING HISTORIES',covered+' / 20','Verified provider-adjusted GBP histories')}${metric('MARKED VALUE',m.covered===20?gbp(m.total):'Incomplete',m.covered+' / 20 positions priced; quantity held constant')}${metric('LAST DATA REFRESH',readableDate(live.data?.retrieved),'GitHub scheduled job, not a live quote')}</div>`+
 note('<strong>'+esc(live.error||s.message||'Loading')+'</strong> '+(errors.length?errors.length+' provider warning(s).':'') ,errors.length||live.error?'warn':'')+
 `<div class="toolbar"><button class="primary" data-pages-check>Check published update</button><button data-auto-latest ${m.covered!==20||m.stale?'disabled':''}>Use latest marked weights in lab</button></div>`+
 `<p class="smallprint">GitHub Actions runs at 07:17, 13:17 and 20:17 UTC and on repository pushes, subject to GitHub scheduling delays and provider availability. The browser checks for newly published data every fifteen minutes. The button above only checks the published copy; it does not scrape markets on demand. Source feed: unlicensed, unofficial Yahoo Finance chart data, Bank of England, FRED and official agency feeds. No fabricated prices are substituted for failures.</p>`+
 (errors.length?`<details open><summary>${errors.length} retrieval warnings</summary>${table(['Feed','Warning'],errors.map(([t,e])=>tr([esc(t),esc(e)])))}</details>`:'')+
 `<details><summary>Market closing prices and coverage</summary>${table(['Holding','Latest close','FX observation',right('Price in GBP'),right('Marked value'),'State'],m.rows.map(({h,m,age,value})=>tr([esc(h.ticker),esc(m?.date||'Unavailable'),esc(m?.fxDate||''),right(m?gbp(m.gbp,4):'—'),right(gbp(value)),esc(!m?'Not retrieved':live.data?.errors?.[h.ticker]||age>7?'Stale / cached':'Retrieved')])))}<p class="smallprint">Dates reflect completed market sessions and are not necessarily identical across exchanges. Dividends and corporate actions are included only insofar as they are in the provider's adjusted prices.</p></details>`+
 `<p class="smallprint">This is a public static GitHub Pages site, not a password-protected account. Do not publish this repository if you wish to conceal positions and values. The original 16 November 2026 holdings ledger is preserved. This does not retrieve brokerage transactions, company valuations, investment trust look-through, or the licensed MSCI World Net Total Return GBP series: SWDA is an ETF proxy. Qualitative research and forward-looking catalysts remain dated ${esc(researchDate)}; official headlines are additional context rather than automatically revised conclusions.</p>`);
}
function officialNews(){
 const news=live.data?.news||[];
 return card('Recently published official policy headlines','Fetched by the scheduled GitHub workflow. Research conclusions below are not automatically revised.',news.length?table(['Authority','Publication','Release'],news.slice(0,60).map(n=>tr([esc(n.source),esc(n.date),`<a href="${/^https?:\/\//.test(n.url)?esc(n.url):'#'}" target="_blank" rel="noopener noreferrer">${esc(n.title)}</a>`]))):note('No official headlines have been fetched yet.','warn'));
}
sources=function(){return health()+originalSources()};
catalysts=function(){return officialNews()+note('Company-specific catalysts and portfolio analysis were researched as of '+researchDate+'. New headlines do not automatically validate or update investment theses.','warn')+originalCatalysts()};
render=function(){originalRender();mount()};
function mount(){
 let strip=document.getElementById('automatic-data-strip');
 if(!strip){strip=document.createElement('div');strip.id='automatic-data-strip';strip.style.cssText='padding:12px 24px;border-bottom:1px solid #26313e;background:#111d29;font-size:13px;display:flex;gap:14px;align-items:center;flex-wrap:wrap';document.querySelector('.topbar')?.insertAdjacentElement('afterend',strip)}
 if(!strip)return;
 const m=liveMarks(),count=ORIGINAL.filter(h=>state.history[h.ticker]?.length).length,bad=Object.keys(live.data?.errors||{}).length||live.error;
 strip.innerHTML=`<strong>${bad?'Data refresh needs attention':count===20?'Market histories loaded':'Awaiting GitHub data publication'}</strong><span class="sub">${count}/20 security histories${m.covered===20?' &middot; latest marked value '+gbp(m.total):''}${live.data?.retrieved?' &middot; updated '+esc(readableDate(live.data.retrieved)):''}</span><button class="small" data-nav="sources">Data sources</button>`;
 const statusEl=$('#header-status');if(statusEl)statusEl.innerHTML=`<span class="dot ${count===20&&!bad?'good':''}"></span>${count?count+'/20 histories':'Automatic market data'}`;
 const footer=document.querySelector('.footer');if(footer)footer.innerHTML=`<span>Research and holdings snapshot: ${esc(researchDate)} &middot; GBP</span><span>Market data last checked: ${esc(readableDate(live.data?.retrieved))} &middot; Research is not auto-rewritten</span>`;
 if(state.page==='overview'){
  const headingEl=document.querySelector('#page .section-head');if(headingEl&&m.covered===20){const el=document.createElement('div');el.className='note';el.innerHTML='<strong>Latest marked portfolio: '+gbp(m.total)+'.</strong> '+(m.stale?'Some observations are stale. ':'')+'The original holdings ledger is preserved; this valuation uses your supplied quantities and latest retrieved closes.';headingEl.insertAdjacentElement('afterend',el)}
 }
}
function applyPayload(p){
 if(!p||typeof p!=='object'||!p.history||!p.historyMeta||!p.marks||!p.errors)throw Error('The published market snapshot has an invalid format.');
 const validated={};for(const [ticker,rows] of Object.entries(p.history)){
  if(!Array.isArray(rows)||rows.length<3)continue;
  let prior='';for(const r of rows){if(!/^\d{4}-\d{2}-\d{2}$/.test(r.date)||!Number.isFinite(r.value)||r.value<=0||r.date<=prior)throw Error('Published data invalid for '+ticker);prior=r.date}
  validated[ticker]=rows;
 }
 live.data=p;live.revision=p.revision??p.retrieved;
 live.status={message:'Published market snapshot loaded',errors:p.errors||{},revision:live.revision,lastAttempt:p.retrieved};
 state.history={...state.history,...validated};state.historyMeta={...state.historyMeta,...p.historyMeta};state.factors={...state.factors,...(p.factors||{})};
 if(p.asOf&&/^\d{4}-\d{2}-\d{2}$/.test(p.asOf))D.asOf=p.asOf;
 live.error='';invalidate();render();
}
async function poll(){
 if(live.polling)return;live.polling=true;
 try{
  if(location.protocol==='file:')throw Error('The standalone HTML needs its accompanying GitHub Actions data publication to update automatically. Open the GitHub Pages website.');
  const response=await fetch('./data/market.json?check='+Date.now(),{cache:'no-store',signal:AbortSignal.timeout(25000)});
  if(!response.ok)throw Error('No market data have been published yet (HTTP '+response.status+'). Check that the GitHub Pages action completed.');
  const payload=await response.json();
  if((payload.revision??payload.retrieved)!==live.revision)applyPayload(payload);
  else if(state.page==='sources')render();else mount();
  live.lastCheck=new Date().toISOString();
 }catch(e){live.error=e.message;live.status.message=e.message;mount()}
 finally{live.polling=false}
}
document.addEventListener('click',e=>{
 const t=e.target.closest('[data-pages-check],[data-auto-latest]');if(!t)return;
 if(t.hasAttribute('data-pages-check')){poll();return}
 try{const m=liveMarks();if(m.covered!==20||m.stale)throw Error('All 20 current holdings need valid, recent prices before updating the allocation.');remember();state.weights=H.map(h=>m.rows.find(r=>r.h.ticker===h.ticker)?.value/m.total||0);state.mode='proposal';invalidate();nav('lab');toast('Latest marked weights loaded. The 2026 snapshot remains unchanged.');}
 catch(err){toast(err.message,true)}
});
window.AutoFeed.apply=applyPayload;window.AutoFeed.poll=poll;window.AutoFeed.liveMarks=liveMarks;
mount();poll();setInterval(poll,15*60*1000);document.addEventListener('visibilitychange',()=>{if(!document.hidden)poll()});
})();
