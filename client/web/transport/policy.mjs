import {PROTOCOL, ProtocolError, canonicalJSON, exactObject} from './wire.mjs';
const fail=code=>{throw new ProtocolError(code);};
// The caller supplies an already-authorized paired destination. Discovery is
// allowed to confirm it, never invent a host, port, path, redirect or trust pin.
export function endpoint(value, authorized, strictLAN=globalThis.USBridgeRuntimeConfig?.strictLAN!==false) {
  if(typeof value!=='string'||value.length>512||typeof authorized!=='string')fail('endpoint_invalid');
  let u,a;try{u=new URL(value);a=new URL(authorized);}catch{fail('endpoint_invalid');}
  for(const x of [u,a]) if(x.protocol!=='https:'||x.username||x.password||x.hash||x.search||x.pathname!=='/webtransport')fail('endpoint_invalid');
  if(strictLAN) {
    const h=u.hostname.replace(/^\[|\]$/g,'').toLowerCase();
    const v4=/^\d+\.\d+\.\d+\.\d+$/.test(h)?h.split('.').map(Number):null;
    const allowed=h==='::1'||(v4&&(v4[0]===127||v4[0]===10||(v4[0]===172&&v4[1]>=16&&v4[1]<=31)||(v4[0]===192&&v4[1]===168)||(v4[0]===169&&v4[1]===254)))||(h.includes(':')&&!h.includes('.')&&(/^f[cd][0-9a-f]{2}:/.test(h)||/^fe[89ab][0-9a-f]:/.test(h)));
    if(!allowed)fail('endpoint_not_local');
  }
  if(u.href!==value||a.href!==authorized||u.href!==a.href)fail('endpoint_not_authorized');
  return u.href;
}
export function discovery(status, body, authorizedEndpoint) {
  if(status===404||status===501)return Object.freeze({available:false,reason:'capability_absent',fallbackAllowed:true});
  if(status===401||status===403)fail('authentication_failed');
  if(status!==200)fail('discovery_failed');
  const c=exactObject(canonicalJSON(body),['protocol','ready','endpoint','tls_mode','codec','audio','input','channels']);
  if(c.protocol!==PROTOCOL||c.tls_mode!=='trusted'||c.codec!=='h264'||c.audio!=='opus'||c.input!==false||!Array.isArray(c.channels)||c.channels.length)fail('capability_unsupported');
  endpoint(c.endpoint,authorizedEndpoint);
  if(c.ready===false)return Object.freeze({available:false,reason:'capability_not_ready',fallbackAllowed:true});
  if(c.ready!==true)fail('capability_invalid');
  return Object.freeze({available:true,endpoint:c.endpoint,protocol:PROTOCOL,tlsMode:'trusted',fallbackAllowed:false});
}
// Trust failures, protocol failures and every error after HELLO are terminal.
// This helper never converts an exception or a server denial into fallback.
export function mayFallback(result, authorizedStreamStarted) {
  return authorizedStreamStarted===false&&result?.available===false&&result.fallbackAllowed===true&&
    (result.reason==='capability_absent'||result.reason==='capability_not_ready');
}
