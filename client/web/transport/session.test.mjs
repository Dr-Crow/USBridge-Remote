import test from 'node:test';
import assert from 'node:assert/strict';
import {negotiateControl,negotiateVideo} from './session.mjs';
import {PROTOCOL,ControlParser,encodeMessage,validateHello} from './wire.mjs';
import {Lifetime} from './lifetime.mjs';
const json=value=>new TextEncoder().encode(JSON.stringify(value));
const deferred=()=>{let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b;});return {promise,resolve,reject};};
const url='https://127.0.0.1:8444/webtransport',origin='https://127.0.0.1:8443';
const welcome={protocol:PROTOCOL,session_id:'session_1',codec:'h264',width:128,height:72,fps:30,audio:{codec:'opus',sample_rate:48000,channels:2},input:false,channels:[]};
const capability={protocol:PROTOCOL,ready:true,endpoint:url,tls_mode:'trusted',codec:'h264',audio:'opus',input:false,channels:[]};
function params(extra={}){return {grant:{protocol:PROTOCOL,session_id:'session_1',ticket:'A'.repeat(43),endpoint:url,origin,ticket_expires_at_ms:Date.now()+1000,session_expires_at_ms:Date.now()+10000,width:128,height:72,fps:30},authorizedEndpoint:url,capabilityStatus:200,capabilityBody:json(capability),...extra};}
function fixture(options={}) {
  const state={starts:0,closes:0,cancels:0,aborts:0,writes:0,acquires:0},ended=deferred(),ready=deferred(),acquired=deferred(),write=deferred();
  let input,output;
  const readable=new ReadableStream({start(c){input=c;},cancel(){state.cancels++;if(options.cancelReject)return Promise.reject(Error('private browser error'));if(options.cancelStuck)return new Promise(()=>{});}});
  const writable=new WritableStream({start(c){output=c;},write(frame){state.writes++;state.frame=frame;const p=new ControlParser([1]);const [message]=p.feed(frame);state.hello=validateHello(message.payload);if(options.writeStuck)return write.promise;if(!options.noWelcome)input.enqueue(encodeMessage(1,json(options.welcome||welcome)));},abort(){state.aborts++;if(options.abortReject)throw Error('private browser error');}});
  const stream={readable,writable};
  class Transport {
    constructor(endpoint,opts){state.starts++;assert.equal(endpoint,url);assert.deepEqual(opts,{congestionControl:'low-latency'});this.closed=ended.promise;if(options.videoIncoming)Object.defineProperty(this,'incomingUnidirectionalStreams',{get(){state.videoReads=(state.videoReads||0)+1;return options.videoIncoming;}});this.ready=options.readyStuck?ready.promise:Promise.resolve();}
    createBidirectionalStream(){state.acquires++;if(options.acquirePending)return acquired.promise;return Promise.resolve(stream);}
    close(){state.closes++;if(!options.closedStuck)ended.resolve();if(options.readyStuck&&!options.readyNeverSettles)ready.reject(Error('closed'));if(options.acquirePending&&!options.acquireNeverSettles)acquired.resolve(stream);if(options.writeStuck&&!options.writeNeverSettles)write.reject(Error('closed'));}
  }
  return {state,stream,input,output,ready,ended,acquired,Transport,options:{Transport,pageOrigin:origin,cleanupTimeoutMs:30}};
}
const tick=()=>new Promise(resolve=>setTimeout(resolve,0));

test('canonical HELLO and matching WELCOME establish a control-only owner, zero sent scratch, join close',async()=>{
  const f=fixture();const owner=await negotiateControl(params(),f.options);assert.equal(owner.mediaStarted,false);assert.deepEqual(owner.welcome,welcome);assert.equal(f.state.hello.ticket,'A'.repeat(43));await tick();assert.ok(f.state.frame.every(n=>n===0));
  const result=await owner.close();assert.deepEqual(result,{closed:true,joined:true,reason:'closed'});assert.equal(await owner.closed,result);assert.equal(f.state.closes,1);assert.equal(f.stream.readable.locked,false);assert.equal(f.stream.writable.locked,false);assert.equal(await owner.close(),result);
});
test('parent cancellation remains active after WELCOME',async()=>{
  const f=fixture(),parent=new AbortController();const owner=await negotiateControl(params({signal:parent.signal}),f.options);parent.abort();assert.equal((await owner.closed).reason,'cancelled');assert.equal(f.state.closes,1);assert.equal(f.stream.readable.locked,false);
});
test('pre-cancelled attempt performs no connection or stream I/O',async()=>{
  const f=fixture(),parent=new AbortController();parent.abort();await assert.rejects(negotiateControl(params({signal:parent.signal}),f.options),{code:'cancelled'});assert.equal(f.state.starts,0);
});
test('capability absence allows no connection; auth and trust errors never become absence',async()=>{
  for(const status of [404,501,401,403,500]){const f=fixture();await assert.rejects(negotiateControl(params({capabilityStatus:status}),f.options),{code:status===404||status===501?'capability_absent':status===401||status===403?'authentication_failed':'discovery_failed'});assert.equal(f.state.starts,0);}
  const f=fixture();await assert.rejects(negotiateControl(params({capabilityBody:json({...capability,tls_mode:'hash'})}),f.options),{code:'capability_unsupported'});assert.equal(f.state.starts,0);
});
test('grant destination, origin, video bounds, expiry and canonical key reject before connection',async()=>{
  for(const patch of [{endpoint:url+'?ticket=x'},{origin:'https://10.0.0.2'},{width:129},{fps:0},{ticket:'B'.repeat(43)},{session_expires_at_ms:Date.now()-1},{session_expires_at_ms:Date.now()+400000}]){const f=fixture(),p=params();Object.assign(p.grant,patch);await assert.rejects(negotiateControl(p,f.options));assert.equal(f.state.starts,0);}
});
test('WELCOME may not widen the authorized dimensions or capabilities',async()=>{
  for(const patch of [{width:256},{session_id:'other'},{input:true},{channels:['clipboard']}]){const f=fixture({welcome:{...welcome,...patch}});await assert.rejects(negotiateControl(params(),f.options));assert.equal(f.state.closes,1);assert.equal(f.stream.readable.locked,false);assert.equal(f.stream.writable.locked,false);}
});
test('setup timeout has no fresh retirement allowance',async()=>{
  const f=fixture({readyStuck:true,readyNeverSettles:true,closedStuck:true}),lifetime=new Lifetime({timeoutMs:25});const start=performance.now();
  try{await assert.rejects(negotiateControl(params({lifetime}),f.options),{code:'session_cleanup_incomplete',primaryCode:'setup_timeout'});assert.ok(performance.now()-start<150);assert.equal(f.state.closes,1);}finally{lifetime.dispose();}
});
test('cancellation of pending acquisition joins a late stream and both releases',async()=>{
  const f=fixture({acquirePending:true}),parent=new AbortController();const p=negotiateControl(params({signal:parent.signal}),f.options);await tick();parent.abort();await assert.rejects(p,{code:'cancelled'});assert.equal(f.state.cancels,1);assert.equal(f.state.aborts,1);assert.equal(f.stream.readable.locked,false);
});
test('failed late stream release cannot produce a clean retirement',async()=>{
  const f=fixture({acquirePending:true,cancelReject:true}),parent=new AbortController();const p=negotiateControl(params({signal:parent.signal}),f.options);await tick();parent.abort();await assert.rejects(p,{code:'session_cleanup_incomplete',primaryCode:'cancelled'});
});
test('post-ready cancellation failure remains observable from terminal promise',async()=>{
  const f=fixture({cancelReject:true});const owner=await negotiateControl(params(),f.options);f.ended.resolve();await assert.rejects(owner.closed,{code:'session_cleanup_incomplete',primaryCode:'peer_closed'});assert.equal(f.state.closes,1);
});
test('unjoined post-ready release is bounded and never reported joined',async()=>{
  const f=fixture({cancelStuck:true});const owner=await negotiateControl(params(),f.options);const start=performance.now();await assert.rejects(owner.close(),{code:'session_cleanup_incomplete'});await assert.rejects(owner.closed);assert.ok(performance.now()-start<200);
});
test('terminal takeover and control protocol failure close the same owner',async()=>{
  for(const [frame,reason] of [[encodeMessage(7,new Uint8Array()),'taken_over'],[encodeMessage(1,json(welcome)),'control_failed'],[new Uint8Array([1,255,255,255,255]),'control_failed']]){const f=fixture();const owner=await negotiateControl(params(),f.options);f.input.enqueue(frame);assert.equal((await owner.closed).reason,reason);assert.equal(f.state.closes,1);}
});
test('grant expiry after setup initiates natural owner cleanup',async()=>{
  const f=fixture(),p=params();p.grant.ticket_expires_at_ms=p.grant.session_expires_at_ms=Date.now()+25;const owner=await negotiateControl(p,f.options);assert.equal((await owner.closed).reason,'expired');assert.equal(f.state.closes,1);
});
test('transport failures are typed and never expose browser error details',async()=>{
  const f=fixture({readyStuck:true});f.ready.reject(Error('secret private endpoint detail'));await assert.rejects(negotiateControl(params(),f.options),error=>error.code==='transport_failed'&&!error.message.includes('secret'));assert.equal(f.state.closes,1);
});

test('separate parent cancel with shared setup budget never starts I/O after retirement',async()=>{
  const f=fixture({readyStuck:true,readyNeverSettles:true}),parent=new AbortController(),lifetime=new Lifetime({timeoutMs:500});
  const p=negotiateControl(params({signal:parent.signal,lifetime}),f.options);await tick();parent.abort();f.ready.resolve();
  try{await assert.rejects(p,{code:'cancelled'});assert.equal(f.state.acquires,0);assert.equal(f.state.writes,0);assert.equal(f.state.closes,1);}finally{lifetime.dispose();}
});

test('borrowed lifetime preserves original parent but its setup timer alone cannot stop an established session',async()=>{
  const f=fixture(),parent=new AbortController(),lifetime=new Lifetime({timeoutMs:20,signal:parent.signal});
  const owner=await negotiateControl(params({lifetime}),f.options);await new Promise(resolve=>setTimeout(resolve,25));assert.equal(lifetime.signal.aborted,true);assert.equal(f.state.closes,0);
  parent.abort();assert.equal((await owner.closed).reason,'cancelled');assert.equal(f.state.closes,1);lifetime.dispose();
});

test('pending HELLO write is joined on cancellation and scratch is cleared after settlement',async()=>{
  const f=fixture({writeStuck:true}),parent=new AbortController();const p=negotiateControl(params({signal:parent.signal}),f.options);await tick();assert.equal(f.state.writes,1);assert.ok(f.state.frame.some(n=>n!==0));parent.abort();await assert.rejects(p,{code:'cancelled'});assert.ok(f.state.frame.every(n=>n===0));assert.equal(f.stream.readable.locked,false);assert.equal(f.stream.writable.locked,false);
});
test('never settled HELLO write is uncertain and retained bytes are not mutated mid-write',async()=>{
  const f=fixture({writeStuck:true,writeNeverSettles:true}),lifetime=new Lifetime({timeoutMs:30});
  try{await assert.rejects(negotiateControl(params({lifetime}),f.options),{code:'session_cleanup_incomplete',primaryCode:'setup_timeout'});assert.ok(f.state.frame.some(n=>n!==0));}finally{lifetime.dispose();}
});
test('expiry during setup rejects WELCOME even when setup budget remains',async()=>{
  const f=fixture({noWelcome:true}),p=params();let now=Date.now();p.grant.ticket_expires_at_ms=p.grant.session_expires_at_ms=now+1000;
  const attempt=negotiateControl(p,{...f.options,now:()=>now});await tick();now+=1001;f.input.enqueue(encodeMessage(1,json(welcome)));await assert.rejects(attempt,{code:'ticket_expired'});assert.equal(f.state.closes,1);
});
test('truncated control EOF before and after WELCOME is fatal and releases locks',async()=>{
  for(const established of [false,true]){const f=fixture({noWelcome:!established});const pending=negotiateControl(params(),f.options);let owner;if(established)owner=await pending;else await tick();f.input.enqueue(new Uint8Array([1,0,0]));f.input.close();if(established)assert.equal((await owner.closed).reason,'control_failed');else await assert.rejects(pending,{code:'control_truncated'});assert.equal(f.stream.readable.locked,false);assert.equal(f.stream.writable.locked,false);}
});
test('writer abort failure cannot produce a joined receipt',async()=>{
  const f=fixture({abortReject:true});const owner=await negotiateControl(params(),f.options);await assert.rejects(owner.close(),{code:'session_cleanup_incomplete'});assert.equal(f.stream.readable.locked,false);assert.equal(f.stream.writable.locked,false);
});

test('wall clock rollback cannot extend the initially authorized elapsed lease',async()=>{
  const f=fixture({noWelcome:true}),p=params();let wall=Date.now(),mono=0;p.grant.ticket_expires_at_ms=p.grant.session_expires_at_ms=wall+1000;
  const pending=negotiateControl(p,{...f.options,now:()=>wall,monotonic:()=>mono});await tick();wall-=5000;mono=1001;f.input.enqueue(encodeMessage(1,json(welcome)));await assert.rejects(pending,{code:'ticket_expired'});
});

test('expired setup still observes rejecting release promises without leaking raw errors',async()=>{
  const f=fixture({noWelcome:true,cancelReject:true,abortReject:true}),lifetime=new Lifetime({timeoutMs:25});const unhandled=[];const onUnhandled=e=>unhandled.push(e);process.on('unhandledRejection',onUnhandled);
  try{await assert.rejects(negotiateControl(params({lifetime}),f.options),{code:'session_cleanup_incomplete',primaryCode:'setup_timeout'});await tick();assert.deepEqual(unhandled,[]);}finally{process.removeListener('unhandledRejection',onUnhandled);lifetime.dispose();}
});

test('grant expiry while transport becomes ready prevents stream acquisition and HELLO',async()=>{
  const f=fixture({readyStuck:true}),p=params();let wall=Date.now();p.grant.ticket_expires_at_ms=p.grant.session_expires_at_ms=wall+1000;
  const pending=negotiateControl(p,{...f.options,now:()=>wall});await tick();wall+=1001;f.ready.resolve();await assert.rejects(pending,{code:'ticket_expired'});assert.equal(f.state.acquires,0);assert.equal(f.state.writes,0);
});

test('established session outlives redemption expiry but closes at authorization expiry',async()=>{
  const f=fixture(),p=params();const now=Date.now();p.grant.ticket_expires_at_ms=now+30;p.grant.session_expires_at_ms=now+80;
  const owner=await negotiateControl(p,f.options);await new Promise(resolve=>setTimeout(resolve,40));assert.equal(f.state.closes,0);assert.equal((await owner.closed).reason,'expired');
});
test('ticket and session expiries are separate exact bounded fields',async()=>{
  for(const patch of [{ticket_expires_at_ms:Date.now()+31000},{ticket_expires_at_ms:Date.now()+20000},{ticket_expires_at_ms:0},{session_expires_at_ms:Date.now()+301000},{expires_at_ms:Date.now()+1000}]){const f=fixture(),p=params();Object.assign(p.grant,patch);await assert.rejects(negotiateControl(p,f.options));assert.equal(f.state.starts,0);}
});

function videoFixture(options={}) {
  let videoInput,stopped=false;const incoming=new ReadableStream({start(c){videoInput=c;}}),f=fixture({...options,videoIncoming:incoming});
  const chunks=[];const sink={accept(packet){chunks.push(packet);},close(){stopped=true;},stats(){return {pending:0,decoderClosed:stopped,cleanupFailures:[],presented:chunks.length};}};
  return {...f,incoming,videoInput,sink,chunks,options:{...f.options,createDecoder:async({lifetime})=>{lifetime.check();return sink;}}};
}
test('video lane opens only after matching WELCOME, parent cancel joins its readers and decoder',async()=>{
 const f=videoFixture({noWelcome:true}),parent=new AbortController(),p=negotiateVideo(params({canvas:{},signal:parent.signal}),f.options);await tick();assert.equal(f.state.videoReads,undefined);f.input.enqueue(encodeMessage(1,json(welcome)));const owner=await p;assert.equal(owner.mediaStarted,true);assert.equal(f.state.videoReads,1);const frame=new ReadableStream();f.videoInput.enqueue(frame);await tick();parent.abort();const closed=await owner.closed;assert.equal(closed.reason,'cancelled');assert.equal(closed.video.decoder.decoderClosed,true);assert.equal(closed.video.active,0);assert.equal(frame.locked,false);assert.equal(f.incoming.locked,false);assert.equal(f.state.closes,1);
});
test('video stream flood closes the entire authenticated owner with original cause',async()=>{
 const f=videoFixture(),owner=await negotiateVideo(params({canvas:{}}),f.options);for(let i=0;i<5;i++)f.videoInput.enqueue(new ReadableStream());const result=await owner.closed;assert.equal(result.reason,'video_stream_budget');assert.equal(result.joined,true);assert.equal(f.state.closes,1);
});
test('late codec initialization is joined and closed on setup cancellation',async()=>{
 const f=videoFixture(),gate=deferred(),parent=new AbortController();let started=false;
 const attempt=negotiateVideo(params({canvas:{},signal:parent.signal}),{...f.options,createDecoder:()=>{started=true;return gate.promise;}});await tick();assert.equal(started,true);parent.abort();gate.resolve(f.sink);await assert.rejects(attempt,{code:'cancelled'});assert.equal(f.sink.stats().decoderClosed,true);assert.equal(f.state.videoReads,undefined);assert.equal(f.state.closes,1);
});
test('unjoined video cancellation consumes only the fixed established retirement budget',async()=>{
 const f=videoFixture(),gate=deferred(),owner=await negotiateVideo(params({canvas:{}}),f.options);f.videoInput.enqueue(new ReadableStream({cancel(){return gate.promise;}}));await tick();const start=performance.now();await assert.rejects(owner.close(),{code:'session_cleanup_incomplete'});assert.ok(performance.now()-start<150);gate.resolve();await tick();assert.equal(f.incoming.locked,false);
});
test('missing incoming video support retires configured decoder and transport',async()=>{
 const f=fixture(),sink={close(){this.stopped=true;},stats(){return {decoderClosed:this.stopped,cleanupFailures:[]};}};await assert.rejects(negotiateVideo(params({canvas:{}}),{...f.options,createDecoder:async()=>sink}),{code:'video_streams_unavailable'});assert.equal(sink.stopped,true);assert.equal(f.state.closes,1);
});

test('throwing codec close cannot skip transport, control and media cancellation',async()=>{const f=videoFixture(),owner=await negotiateVideo(params({canvas:{}}),f.options);f.sink.close=()=>{throw Error('private');};await assert.rejects(owner.close(),{code:'session_cleanup_incomplete'});assert.equal(f.state.closes,1);assert.equal(f.incoming.locked,false);assert.equal(f.stream.readable.locked,false);assert.equal(f.stream.writable.locked,false);});

test('shared authenticated owner preserves production lifecycle and exact grant',async()=>{
  const {ownAuthenticatedControl}=await import('./session_owner.mjs');
  const observed=[];
  for(const start of [negotiateControl,ownAuthenticatedControl]) {
    const f=fixture();const p=params();const session=await start(p,f.options);
    assert.deepEqual(f.state.hello,{protocol:p.grant.protocol,session_id:p.grant.session_id,ticket:p.grant.ticket});
    const result=await session.close();
    observed.push({result,starts:f.state.starts,closes:f.state.closes,cancels:f.state.cancels,aborts:f.state.aborts,writes:f.state.writes});
  }
  assert.deepEqual(observed[0],observed[1]);
});
test('production entrypoint refuses fixture trust and unauthenticated capability before native construction',async()=>{
  for(const mode of ['fixture-pinned-leaf','hash','untrusted']) {
    const f=fixture();await assert.rejects(negotiateControl(params({capabilityBody:json({...capability,tls_mode:mode})}),f.options),e=>e.code==='capability_unsupported');assert.equal(f.state.starts,0);
  }
  for(const status of [401,403]) {
    const f=fixture();await assert.rejects(negotiateControl(params({capabilityStatus:status}),f.options),e=>e.code==='authentication_failed');assert.equal(f.state.starts,0);
  }
});
