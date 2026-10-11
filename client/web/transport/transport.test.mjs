import test from 'node:test';
import assert from 'node:assert/strict';
import {PROTOCOL,LIMITS,ControlParser,encodeMessage,canonicalJSON,validateHello,validateWelcome,parseAudio,parseVideo,LatestIDR,ProtocolError} from './wire.mjs';
import {endpoint,discovery,mayFallback} from './policy.mjs';
import {Lifetime,readBounded} from './lifetime.mjs';
import {checkProbe} from './worker.mjs';
const enc=new TextEncoder();const json=v=>enc.encode(JSON.stringify(v));
const id='session_1';const ticket='A'.repeat(43);
const hello={protocol:PROTOCOL,session_id:id,ticket};
const welcome={protocol:PROTOCOL,session_id:id,codec:'h264',width:128,height:72,fps:30,audio:{codec:'opus',sample_rate:48000,channels:2},input:false,channels:[]};
const url='https://127.0.0.1:8444/webtransport';
const capability={protocol:PROTOCOL,ready:true,endpoint:url,tls_mode:'trusted',codec:'h264',audio:'opus',input:false,channels:[]};
const rejects=fn=>assert.throws(fn,ProtocolError);

test('canonical control JSON rejects duplicates, aliases, invalid UTF8 and BOM',()=>{
  assert.deepEqual(validateHello(json(hello)),hello);
  for(const raw of ['{"a":1,"a":1}',' {"a":1}','{"a":1.0}','{"a":"\\u0062"}',JSON.stringify({...hello,CAPTURE_CONSENT:true})])rejects(()=>validateHello(enc.encode(raw)));
  for(const raw of [new Uint8Array([0xff]),new Uint8Array([239,187,191,...json(hello)]),new Uint8Array(4097)])rejects(()=>canonicalJSON(raw));
  for(const patch of [{ticket:'A'.repeat(42)+'B'},{ticket:'A'.repeat(43)+'='},{ticket:'A'.repeat(42)+'\n'},{session_id:'a.b'},{protocol:'legacy'}])rejects(()=>validateHello(json({...hello,...patch})));
});
test('welcome requires exact session and narrow actual capabilities',()=>{
  assert.equal(validateWelcome(json(welcome),id).codec,'h264');
  for(const patch of [{input:true},{channels:['clipboard']},{width:129},{height:0},{fps:61},{codec:'h265'},{session_id:'other'},{audio:{codec:'opus',sample_rate:48000,channels:99}},{extra:true}])rejects(()=>validateWelcome(json({...welcome,...patch}),id));
});
test('control messages survive every split without excess buffering',()=>{
  const frame=encodeMessage(1,json(welcome));
  for(let at=0;at<=frame.length;at++){const p=new ControlParser();const result=[...p.feed(frame.subarray(0,at)),...p.feed(frame.subarray(at))];p.end();assert.equal(result.length,1);assert.deepEqual(result[0].payload,json(welcome));}
  const p=new ControlParser();assert.deepEqual(p.feed(encodeMessage(7,new Uint8Array())),[{type:7,payload:new Uint8Array()}]);p.end();
});
test('control parser rejects huge/truncated/unknown/flooded messages',()=>{
  rejects(()=>new ControlParser().feed(new Uint8Array([1,255,255,255,255])));
  rejects(()=>new ControlParser().feed(new Uint8Array([9,0,0,0,0])));
  rejects(()=>new ControlParser().feed(new Uint8Array(65537)));
  rejects(()=>{const p=new ControlParser();p.feed(new Uint8Array([1,0,0,0,2,1]));p.end();});
  const flood=new Uint8Array(65*5);for(let i=0;i<65;i++)flood[i*5]=7;rejects(()=>new ControlParser().feed(flood));
  for(let i=4097;i<14100;i++) {const h=new Uint8Array(5);h[0]=1;new DataView(h.buffer).setUint32(1,i);rejects(()=>new ControlParser().feed(h));}
});
const video=(seq,ts)=>{const b=new Uint8Array(18);b[0]=1;b[1]=1;const d=new DataView(b.buffer);d.setUint32(2,seq);d.setBigUint64(6,BigInt(ts));b.set([0,0,1,0x65],14);return b;};
test('all-IDR ordering drops late frames and never decodes backwards',()=>{
  const q=new LatestIDR();assert.equal(q.accept(video(2,2)).sequence,2);assert.equal(q.accept(video(1,1)),null);assert.equal(q.accept(video(2,2)),null);assert.equal(q.accept(video(4,4)).sequence,4);rejects(()=>q.accept(video(5,3)));
  rejects(()=>parseVideo(video(0,1)));rejects(()=>parseVideo(video(1,Number.MAX_SAFE_INTEGER+1)));
  rejects(()=>parseVideo(new Uint8Array(LIMITS.video+15)));
  const delta=video(1,1);delta[1]=0;rejects(()=>parseVideo(delta));
});
test('audio shape and codec packet budget are explicit',()=>{
  const packet=new Uint8Array(10);packet[0]=2;packet[9]=0xf8;assert.equal(parseAudio(packet).data.length,1);
  for(const b of [new Uint8Array(9),new Uint8Array(LIMITS.opus+10),new Uint8Array(10)])rejects(()=>parseAudio(b));
});
test('destination identity and trust cannot be invented by discovery',()=>{
  assert.equal(endpoint(url,url),url);
  for(const target of ['http://127.0.0.1/webtransport','https://127.0.0.1:8445/webtransport',url+'?ticket=secret',url+'#x','https://name.local/webtransport','https://8.8.8.8/webtransport','https://user@127.0.0.1/webtransport','https://127.0.0.1:8444/other'])rejects(()=>endpoint(target,url));
  assert.equal(discovery(200,json(capability),url).available,true);
  rejects(()=>discovery(200,json({...capability,tls_mode:'hash'}),url));
  rejects(()=>discovery(200,json({...capability,endpoint:'https://10.0.0.2/webtransport'}),url));
});
test('fallback only follows capability absence before authorized stream',()=>{
  for(const status of [404,501]){const r=discovery(status,new Uint8Array(),url);assert.equal(mayFallback(r,false),true);assert.equal(mayFallback(r,true),false);}
  const r=discovery(200,json({...capability,ready:false}),url);assert.equal(mayFallback(r,false),true);
  for(const status of [401,403,500,503])rejects(()=>discovery(status,json(capability),url));
  assert.equal(mayFallback(new ProtocolError('trust_failed'),false),false);
});
test('one setup deadline is not renewed by sequential stages',async()=>{
  let now=0;const l=new Lifetime({timeoutMs:100,clock:()=>now});
  try{await l.run(()=>{now=60;return 1;});await assert.rejects(l.run(()=>{now=101;return 2;}),{code:'setup_timeout'});}
  finally{l.dispose();}
});
test('pre-cancelled and in-flight attempts fail and release readers',async()=>{
  const parent=new AbortController();parent.abort();const a=new Lifetime({signal:parent.signal});
  try{await assert.rejects(a.run(()=>assert.fail('operation started')),{code:'cancelled'});}finally{a.dispose();}
  const c=new AbortController();const l=new Lifetime({timeoutMs:1000,signal:c.signal});let cancelled=false;
  const stream=new ReadableStream({cancel(){cancelled=true;}});
  const p=readBounded(stream,32,l);c.abort();await assert.rejects(p);assert.equal(cancelled,true);assert.equal(stream.locked,false);l.dispose();
});
test('bounded reader rejects oversized/chunk floods and releases lock',async()=>{
  const l=new Lifetime();const stream=new ReadableStream({start(c){c.enqueue(new Uint8Array(33));}});
  try{await assert.rejects(readBounded(stream,32,l),{code:'read_size'});assert.equal(stream.locked,false);}finally{l.dispose();}
});
test('static worker protocol exposes no media capability',()=>{
  assert.deepEqual(checkProbe({type:'probe',protocol:PROTOCOL}),{type:'ready',protocol:PROTOCOL,mediaEnabled:false});
  rejects(()=>checkProbe({type:'probe',protocol:PROTOCOL,ticket:'secret'}));rejects(()=>checkProbe({type:'capture',protocol:PROTOCOL}));
});

test('unjoined reader cancellation fails by the original deadline',async()=>{
  const l=new Lifetime({timeoutMs:30});
  const stream=new ReadableStream({start(c){c.enqueue(new Uint8Array(33));},cancel(){return new Promise(()=>{});}});
  const start=performance.now();
  try{await assert.rejects(readBounded(stream,32,l),{code:'cleanup_deadline',primaryCode:'read_size'});assert.ok(performance.now()-start<500);assert.equal(stream.locked,false);}finally{l.dispose();}
});

test('static API is frozen and advertises the unfinished transport honestly',async()=>{
  const {API}=await import('./player.mjs');assert.equal(Object.isFrozen(API),true);assert.equal(API.transportEnabled,false);assert.equal(API.supported(),false);
  const parent=new AbortController();parent.abort();await assert.rejects(API.probeWorker({signal:parent.signal}),{code:'cancelled'});
});

test('worker probe consumes an existing absolute setup deadline without reset',async()=>{
  const {API}=await import('./player.mjs');let now=0;const lifetime=new Lifetime({timeoutMs:100,clock:()=>now});now=101;
  try{await assert.rejects(API.probeWorker({lifetime}),{code:'setup_timeout'});}finally{lifetime.dispose();}
});

test('cleanup checks the same absolute deadline before and after synchronous settlement',async()=>{
  let now=0;const lifetime=new Lifetime({timeoutMs:100,clock:()=>now});now=90;
  try{await assert.rejects(lifetime.retire(()=>{now=101;return 'joined';}),{code:'cleanup_deadline'});}finally{lifetime.dispose();}
});
