const quickConfigPage = r'''<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Orbit 快捷配置</title><style>
*{box-sizing:border-box}body{margin:0;background:#f5f5ef;color:#102827;font:16px system-ui,sans-serif}
main{max-width:540px;margin:32px auto;padding:24px}h1{font-size:28px}p{line-height:1.6;color:#52645c}
label{display:block;margin-top:20px;font-weight:600}input{display:block;width:100%;margin-top:8px;padding:14px;border:1px solid #b6c5bc;border-radius:10px;font:inherit;background:white}
small{display:block;margin-top:6px;color:#52645c}.actions{display:flex;flex-wrap:wrap;gap:12px;margin-top:24px}button{padding:13px 20px;border:1px solid #24634c;border-radius:24px;background:#24634c;color:white;font:inherit;cursor:pointer}button:first-child{background:white;color:#24634c}button:disabled{opacity:.5}#message{white-space:pre-wrap}
</style></head><body><main><h1>Orbit 快捷配置</h1><p>在这里配置设备的 MQTT 连接。请保持设备上的二维码弹窗打开，并连接同一局域网。</p>
<form id="form"><fieldset id="fields" disabled style="border:0;padding:0;margin:0">
<label>MQTT 服务地址<input id="uri" required placeholder="ssl://mqtt.example.com:8883" autocapitalize="none" spellcheck="false"></label><small>支持 ssl://、mqtts://、tcp://、mqtt://，需填写端口。</small>
<label>Node ID<input id="nodeId" required pattern="[A-Za-z0-9_-]{1,64}" maxlength="64" autocapitalize="none" spellcheck="false"></label><small>与 Core 的 projection_routes 中 node_id 一致。</small>
<label>用户名（可选）<input id="username" autocomplete="off" autocapitalize="none" spellcheck="false"></label>
<label>密码（可选）<input id="password" type="password" autocomplete="off"></label>
<div class="actions"><button type="button" id="test">测试连接</button><button type="submit">保存配置</button></div></fieldset></form><p id="message" role="status" aria-live="polite">正在读取设备配置…</p><small>保存至设备同一份加密配置。未启动同步时，请在设备上点击「开始同步」。仅在可信局域网使用。</small></main>
<script>
const token=location.hash.slice(1);
const fields=document.getElementById('fields'),form=document.getElementById('form'),message=document.getElementById('message');
const keys=['uri','nodeId','username','password'];
async function request(path,config){const response=await fetch(path,{method:config?'POST':'GET',headers:{'X-Orbit-Token':token,'Content-Type':'application/json'},body:config?JSON.stringify(config):undefined});const data=await response.json();if(!response.ok)throw Error(data.message||'操作失败');return data;}
async function act(path){if(!form.reportValidity())return;const config={};keys.forEach(key=>config[key]=document.getElementById(key).value);fields.disabled=true;message.textContent=path==='/test'?'正在从设备测试 MQTT 连接…':'正在保存…';try{message.textContent=(await request(path,config)).message;}catch(error){message.textContent=error.message==='Failed to fetch'?'无法连接设备，请保持弹窗打开或重新扫码。':error.message;}finally{fields.disabled=false;}}
document.getElementById('test').onclick=()=>act('/test');form.onsubmit=event=>{event.preventDefault();act('/save');};
request('/config').then(config=>{keys.forEach(key=>document.getElementById(key).value=config[key]||'');fields.disabled=false;message.textContent='已读取设备当前表单，可测试或保存。';}).catch(()=>{message.textContent='无法读取配置，请重新扫描设备二维码。';});
</script></body></html>''';
