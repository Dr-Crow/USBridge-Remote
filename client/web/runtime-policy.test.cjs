const {test} = require('node:test');
const assert = require('node:assert/strict');
const vm = require('node:vm');
const fs = require('node:fs');
const source = fs.readFileSync(__dirname + '/runtime-policy.js', 'utf8');
function setup(config) {
  const calls=[];
  class RTC {constructor(c){this.config=c;} setConfiguration(c){this.config=c;}}
  class WT {constructor(url,options){calls.push({transport:url,options});this.url=url;}}
  class WS {constructor(url){this.url=url;}}
  const c={URL, location:{href:'https://192.168.1.5:8443/'}, USBridgeRuntimeConfig:config, fetch:(...a)=>{calls.push(a);return Promise.resolve('ok');}, RTCPeerConnection:RTC, WebSocket:WS, WebTransport:WT};
  vm.runInNewContext(source,c); return {c,calls};
}
test('strict requests reject public destinations before transport and forbid redirects',async()=>{
 const {c,calls}=setup({strictLAN:true});
 for(const url of ['https://example.com/','https://8.8.8.8/','http://192.168.1.1/','https://[::ffff:8.8.8.8]/','https://user:pass@192.168.1.1/']) await assert.rejects(c.fetch(url));
 assert.equal(calls.length,0);
 await c.fetch('/models/model.onnx',{redirect:'follow'});
 assert.equal(calls[0][1].redirect,'error');
 for(const url of ['https://[fd00::1]/','https://[fe80::1]/','http://127.0.0.1/']) await c.fetch(url);
});
test('strict RTC stays host-only and WebSocket requires local TLS',()=>{
 const {c}=setup();
 assert.throws(()=>new c.RTCPeerConnection({iceServers:[{urls:'stun:public'}]}));
 const pc=new c.RTCPeerConnection({}); assert.equal(pc.config.iceServers.length,0);
 assert.throws(()=>pc.setConfiguration({iceServers:[{urls:'turn:public'}]}));
 assert.throws(()=>new c.WebSocket('wss://public.example/'));
 assert.equal(new c.WebSocket('wss://10.0.0.1/').url,'wss://10.0.0.1/');
});
test('explicit normal mode preserves existing transport',async()=>{
 const {c,calls}=setup({strictLAN:false}); await c.fetch('https://example.com/'); assert.equal(calls.length,1);
});

test('strict WebTransport validates TLS path and local destination before constructor',()=>{
 const {c,calls}=setup();
 for(const url of ['https://public.example/webtransport','https://8.8.8.8/webtransport','http://127.0.0.1/webtransport','https://127.0.0.1/other','https://127.0.0.1/webtransport?ticket=secret']) assert.throws(()=>new c.WebTransport(url));
 assert.throws(()=>new c.WebTransport('https://127.0.0.1/webtransport',{serverCertificateHashes:[]}));
 assert.throws(()=>new c.WebTransport('https://127.0.0.1/webtransport',Object.create({serverCertificateHashes:[]})));
 assert.throws(()=>new c.WebTransport('https://127.0.0.1/webtransport',{allowPooling:true}));
 assert.equal(calls.length,0);
 assert.equal(new c.WebTransport('https://127.0.0.1/webtransport',{congestionControl:'low-latency'}).url,'https://127.0.0.1/webtransport');
 assert.equal(calls.length,1);
});
