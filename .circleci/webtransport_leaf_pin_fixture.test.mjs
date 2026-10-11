import test from 'node:test';
import assert from 'node:assert/strict';
import {makePinnedLeafTransport,FIXTURE_AUTHENTICATION} from './webtransport_leaf_pin_fixture.mjs';
const time=1700000000000;
const config=()=>({role:'webtransport-pinned-leaf-test-v1',protocol:'usbridge-webtransport-v1',endpoint:'https://127.0.0.1:8444/webtransport',origin:'https://127.0.0.1:8443',leaf_sha256:'ab'.repeat(32),leaf_not_before_ms:time-1000,leaf_not_after_ms:time+3599000,session_id:'synthetic',ticket:'A'.repeat(43),ticket_expires_at_ms:time+15000,session_expires_at_ms:time+15000,width:128,height:72,fps:30,public_client_commit:'a'.repeat(40),private_fixture_sha256:'b'.repeat(64)});
const deps=(NativeTransport=class{})=>({NativeTransport,now:()=>time,pageOrigin:'https://127.0.0.1:8443',expectedPublicClientCommit:'a'.repeat(40),expectedPrivateFixtureSHA256:'b'.repeat(64)});
test('exact full-DER digest is one-use, dedicated and explicitly fixture authenticated',()=>{
  let calls=0;const c=config();const p=makePinnedLeafTransport(c,deps(class{constructor(url,options){calls++;assert.equal(url,c.endpoint);assert.equal(options.allowPooling,false);assert.equal(options.congestionControl,'low-latency');assert.equal(options.serverCertificateHashes.length,1);assert.equal(options.serverCertificateHashes[0].algorithm,'sha-256');assert.deepEqual(options.serverCertificateHashes[0].value,new Uint8Array(32).fill(0xab));}}));
  assert.equal(p.authentication,FIXTURE_AUTHENTICATION);assert(Object.isFrozen(p));new p.Transport(c.endpoint);assert.equal(calls,1);assert.throws(()=>new p.Transport(c.endpoint),/reused/);assert.equal(calls,1);
});
test('untrusted configuration mutations fail before any native I/O',()=>{
  const changes=[
    {role:'production'},{protocol:'other'},{leaf_sha256:'AB'.repeat(32)},{leaf_sha256:'a'.repeat(63)},{leaf_sha256:'g'.repeat(64)},{leaf_sha256:['ab'.repeat(32)]},
    {endpoint:'https://localhost:8444/webtransport'},{endpoint:'https://127.0.0.2:8444/webtransport'},{endpoint:'http://127.0.0.1:8444/webtransport'},{endpoint:'https://127.0.0.1:8444/other'},{endpoint:'https://127.0.0.1:8444/webtransport?ticket=x'},{endpoint:'https://user@127.0.0.1:8444/webtransport'},
    {origin:'https://127.0.0.1:9999'},{origin:'https://127.0.0.1:8443/'},{origin:'http://127.0.0.1:8443'},
    {leaf_not_before_ms:time+1},{leaf_not_after_ms:time},{leaf_not_after_ms:time+3600000},{leaf_not_before_ms:0},
    {ticket_expires_at_ms:time},{ticket_expires_at_ms:time+30001},{session_expires_at_ms:time+300001},{session_expires_at_ms:time+1},
    {ticket:'secret'},{session_id:'../bad'},{width:1920},{height:1080},{fps:60},{public_client_commit:'main'},{private_fixture_sha256:'latest'},
    {algorithm:'sha-1'},{serverCertificateHashes:[]},{tls_mode:'trusted'}
  ];
  for(const change of changes){let calls=0;assert.throws(()=>makePinnedLeafTransport({...config(),...change},deps(class{constructor(){calls++;}})));assert.equal(calls,0,JSON.stringify(Object.keys(change)));}
  const missing=config();delete missing.leaf_sha256;assert.throws(()=>makePinnedLeafTransport(missing,deps()));
});
test('constructor rechecks exact scope and expiry and never accepts caller trust options',()=>{
  let now=time,calls=0;const c=config();const p=makePinnedLeafTransport(c,{...deps(class{constructor(){calls++;}}),now:()=>now});
  assert.throws(()=>new p.Transport(c.endpoint,{congestionControl:'low-latency',serverCertificateHashes:[]}),/fields/);
  assert.throws(()=>new p.Transport('https://127.0.0.1:8445/webtransport'),/scope/);
  now=c.ticket_expires_at_ms;assert.throws(()=>new p.Transport(c.endpoint),/expiry/);assert.equal(calls,0);
});
test('configuration mutations cannot alter the authorized captured identity',()=>{
  const c=config();let observed;const p=makePinnedLeafTransport(c,deps(class{constructor(url,options){observed={url,options};}}));
  c.endpoint='https://127.0.0.1:9000/webtransport';c.leaf_sha256='cd'.repeat(32);
  new p.Transport('https://127.0.0.1:8444/webtransport');assert.equal(observed.url,'https://127.0.0.1:8444/webtransport');assert.equal(observed.options.serverCertificateHashes[0].value[0],0xab);
});
test('native trust rejection is terminal, with no retry or automatic fallback',()=>{
  let calls=0;const c=config();const p=makePinnedLeafTransport(c,deps(class{constructor(){calls++;throw new Error('synthetic native trust failure');}}));
  assert.throws(()=>new p.Transport(c.endpoint),/synthetic native trust failure/);assert.throws(()=>new p.Transport(c.endpoint),/reused/);assert.equal(calls,1);
});

test('caller expected provenance must match and returned grant is immutable',()=>{
 const c=config();
 for(const d of [{...deps(),expectedPublicClientCommit:'c'.repeat(40)},{...deps(),expectedPrivateFixtureSHA256:'d'.repeat(64)},{...deps(),expectedPublicClientCommit:undefined}])assert.throws(()=>makePinnedLeafTransport(c,d),/provenance_mismatch/);
 const p=makePinnedLeafTransport(c,deps());assert(Object.isFrozen(p.grant));assert.equal(p.grant.ticket,c.ticket);assert.equal(p.grant.session_id,c.session_id);
 c.ticket='B'.repeat(43);assert.equal(p.grant.ticket,'A'.repeat(43));assert.throws(()=>{p.grant.endpoint='https://127.0.0.1:9999/webtransport';},TypeError);
});

test('fixture composition sends its exact immutable grant through the shared lifecycle owner',async()=>{
 const {ownAuthenticatedControl}=await import('../client/web/transport/session_owner.mjs');
 const {ControlParser,encodeMessage,validateHello}=await import('../client/web/transport/wire.mjs');
 const c=config();let wire,resolveClosed,input,calls=0;
 class NativeTransport {
  constructor(endpoint,options){calls++;assert.equal(endpoint,c.endpoint);assert.equal(options.serverCertificateHashes[0].value[0],0xab);this.ready=Promise.resolve();this.closed=new Promise(r=>resolveClosed=r);}
  createBidirectionalStream(){return Promise.resolve({readable:new ReadableStream({start(c){input=c;}}),writable:new WritableStream({write(frame){const [m]=new ControlParser([1]).feed(frame);wire=validateHello(m.payload);input.enqueue(encodeMessage(1,new TextEncoder().encode(JSON.stringify({protocol:c.protocol,session_id:c.session_id,codec:'h264',width:128,height:72,fps:30,audio:{codec:'opus',sample_rate:48000,channels:2},input:false,channels:[]}))));}})});}
  close(){resolveClosed();}
 }
 const p=makePinnedLeafTransport(c,deps(NativeTransport));
 const session=await ownAuthenticatedControl({grant:p.grant,authorizedEndpoint:p.grant.endpoint},{Transport:p.Transport,pageOrigin:c.origin,now:()=>time});
 assert.deepEqual(wire,{protocol:p.grant.protocol,session_id:p.grant.session_id,ticket:p.grant.ticket});
 const receipt=await session.close();assert.equal(receipt.joined,true);assert.equal(calls,1);assert.equal(p.authentication,'fixture-pinned-leaf');
});
