'use strict';
const $ = id => document.getElementById(id);
let revision = 0, dirty = false;
function notice(text, error = false) { $('notice').textContent = text; $('notice').className = error ? 'error' : ''; }
async function api(path, options) { const response = await fetch(path, options); if (!response.ok) throw new Error(await response.text()); return response.json(); }
function el(tag, text, cls) { const node = document.createElement(tag); if (text) node.textContent = text; if (cls) node.className = cls; return node; }
function date(value) { return value ? new Date(value).toLocaleString() : '—'; }
function card(title, lines, tags) { const node = el('article', '', 'card'); node.append(el('h3', title)); lines.forEach(line => node.append(el('div', line, 'meta'))); const row = el('div', '', 'tags'); tags.forEach(tag => row.append(el('span', tag.text, 'tag' + (tag.fresh ? ' fresh' : '')))); node.append(row); return node; }
function renderList(id, items, empty) { $(id).replaceChildren(...items); if (!items.length) $(id).append(el('div', empty, 'empty')); }
async function refresh() {
 try {
  const state = await api('/api/state');
  $('connection').textContent = '● 已连接 · ' + new Date().toLocaleTimeString();
  $('core').textContent = state.core_id + ' / ' + state.core_epoch;
  $('agent-count').textContent = state.agents.length; $('node-count').textContent = state.nodes.length;
  renderList('agents', state.agents.map(agent => {
   const a = agent.state;
   const tags = (a.sources || []).map(source => {const kind = source.observationType === 'OBSERVATION_TYPE_CODEX' ? 'codex' : 'usage'; return {text: kind + ' · ' + (source.enabled ? (source.health || 'UNKNOWN').replace('SOURCE_HEALTH_', '') : '已禁用'), fresh: source.enabled && source.health === 'SOURCE_HEALTH_HEALTHY'};});
   tags.push({text:'usage ' + (agent.usage_fresh ? '新鲜' : '无新鲜数据'), fresh:agent.usage_fresh}, {text:'codex ' + (agent.codex_fresh ? '新鲜' : '无新鲜数据'), fresh:agent.codex_fresh});
   return card(a.hostLabel, [a.agentId, '版本 ' + a.agentVersion, '状态产生于 ' + date(a.metadata?.producedAt), ...(a.sources || []).map(source => (source.observationType || '').replace('OBSERVATION_TYPE_', '') + ' 最近成功 ' + date(source.lastSuccessAt) + (source.errorCode ? ' · ' + source.errorCode : ''))], tags);
  }), '尚未发现 Agent，等待 MQTT 状态。');
  renderList('nodes', state.nodes.map(n => card(n.nodeId, [n.seriesId + ' / ' + n.modelId + ' / ' + n.variantId, '版本 ' + n.firmwareVersion, '状态产生于 ' + date(n.metadata?.producedAt)], [{text:'已发现 · 连接状态未知'}])), '尚未发现 Node，规则仍可预先配置。');
  $('agent-options').replaceChildren(...state.agents.map(a => {const o=el('option');o.value=a.id;return o;}));
  $('node-options').replaceChildren(...state.nodes.map(n => {const o=el('option');o.value=n.nodeId;return o;}));
 } catch(error) { $('connection').textContent = '连接中断'; notice(error.message, true); }
}
function field(label, type, value, list) { const wrap=el('label',label),input=el(type);input.value=value || '';if(list)input.setAttribute('list',list);wrap.append(input);return {wrap,input}; }
function addRule(id='', route={profile:'overview-web',inputs:[]}) {
 const row=el('div','','rule');
 const node=field('NODE ID','input',id,'node-options');node.input.required=true;node.input.pattern='[a-z0-9][a-z0-9_-]{0,63}';node.input.setAttribute('aria-label','Node ID');
 const profile=field('视图 PROFILE','select');['usage-oled-128x32','overview-web','overview-android'].forEach(value=>{const o=el('option',value);o.value=value;profile.input.append(o);});profile.input.value=route.profile;
 const usage=field('USAGE 来源 AGENT','input',route.inputs.find(i=>i.observation_type==='usage')?.agent_id,'agent-options');
 const codex=field('CODEX 来源 AGENT','input',route.inputs.find(i=>i.observation_type==='codex')?.agent_id,'agent-options');
 usage.input.placeholder='不转发 usage';codex.input.placeholder='不转发 codex';
 const remove=el('button','删除','remove');remove.type='button';remove.onclick=()=>{row.remove();changed();};
 row.append(node.wrap,profile.wrap,usage.wrap,codex.wrap,remove);
 row.read=()=>[node.input.value.trim(),{profile:profile.input.value,inputs:[{agent_id:usage.input.value.trim(),observation_type:'usage'},{agent_id:codex.input.value.trim(),observation_type:'codex'}].filter(i=>i.agent_id)}];
 const update=()=>{codex.input.disabled=profile.input.value==='usage-oled-128x32';if(codex.input.disabled)codex.input.value='';};profile.input.addEventListener('change',update);update();row.addEventListener('input',changed);row.addEventListener('change',changed);$('rules').append(row);
}
function changed() { dirty=true;notice('有未保存的规则修改。'); }
async function loadRoutes() { try {const d=await api('/api/routes');revision=d.revision;$('rules').replaceChildren();Object.entries(d.routes).forEach(([id,route])=>addRule(id,route));dirty=false;$('route-count').textContent=Object.keys(d.routes).length;$('revision').textContent='配置版本 '+revision;notice('规则已载入。');}catch(error){notice(error.message,true);} }
$('add').onclick=()=>{addRule();changed();};
$('reload').onclick=()=>{if(!dirty || confirm('放弃未保存的修改并重新载入？'))loadRoutes();};
$('refresh').onclick=refresh;
$('save').onclick=async()=>{
 const routes={};
 for(const row of $('rules').children){const [id,route]=row.read();if(!/^[a-z0-9][a-z0-9_-]{0,63}$/.test(id)){notice('请填写有效的 Node ID。',true);return;}if(Object.hasOwn(routes,id)){notice('同一个 Node 只能有一条规则：'+id,true);return;}routes[id]=route;}
 const controls = [...document.querySelectorAll('button,input,select')].filter(control => !control.disabled);
 controls.forEach(control => control.disabled=true);
 try{const d=await api('/api/routes',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({revision,routes})});revision=d.revision;dirty=false;$('revision').textContent='配置版本 '+revision;$('route-count').textContent=Object.keys(routes).length;notice(d.publish_pending?'规则已保存并生效，设备投递待重试。':'规则已保存并生效。');}catch(error){notice(error.message,true);}finally{controls.forEach(control => control.disabled=false);}
};
window.addEventListener('beforeunload',event=>{if(dirty){event.preventDefault();event.returnValue='';}});
refresh();loadRoutes();setInterval(refresh,5000);
