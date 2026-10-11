import test from 'node:test';import assert from 'node:assert/strict';
import {receiveVideo,observeStreamTerminal,retireUnclaimedReadable} from './streams.mjs';
const tick=()=>new Promise(r=>setTimeout(r,0));
function fixture(){let queue;const incoming=new ReadableStream({start(c){queue=c;}});const accepted=[],errors=[];let stopped=false;const sink={accept(p){accepted.push(p);},close(){stopped=true;},stats(){return {decoderClosed:stopped,cleanupFailures:[]};}};const owner=receiveVideo({incomingUnidirectionalStreams:incoming},sink,{onFailure:e=>errors.push(e.code),frameTimeoutMs:100});return {incoming,queue,accepted,errors,sink,owner};}
test('bounded frames are assembled and every stream lock retires on close',async()=>{const f=fixture();const streams=[];for(let n=0;n<3;n++){const s=new ReadableStream({start(c){c.enqueue(new Uint8Array([1,n]));c.enqueue(new Uint8Array([2]));c.close();}});streams.push(s);f.queue.enqueue(s);}await tick();assert.equal(f.accepted.length,3);assert.ok(streams.every(s=>!s.locked));const r=await f.owner.stop();assert.equal(r.joined,true);assert.equal(r.streams,3);assert.equal(f.incoming.locked,false);assert.equal(f.owner.stats().active,0);});
test('fifth simultaneous frame fails and cancels all owned readers',async()=>{const f=fixture(),streams=[];let cancelled=0;for(let n=0;n<5;n++){const s=new ReadableStream({cancel(){cancelled++;}});streams.push(s);f.queue.enqueue(s);}const r=await f.owner.closed;assert.equal(r.reason,'video_stream_budget');assert.deepEqual(f.errors,['video_stream_budget']);assert.equal(cancelled,5);assert.ok(streams.every(s=>!s.locked));assert.equal(f.incoming.locked,false);});
test('oversized and fragmented frames fail before sink receives bytes',async()=>{for(const mode of ['size','fragments']){const f=fixture();const s=new ReadableStream({start(c){if(mode==='size')c.enqueue(new Uint8Array(1048591));else for(let i=0;i<1025;i++)c.enqueue(new Uint8Array());c.close();}});f.queue.enqueue(s);const r=await f.owner.closed;assert.equal(r.reason,'video_stream_size');assert.equal(f.accepted.length,0);assert.equal(s.locked,false);}});
test('stalled frame timeout cancels and joins instead of resetting authorization',async()=>{const f=fixture(),s=new ReadableStream();f.queue.enqueue(s);const r=await f.owner.closed;assert.equal(r.reason,'video_frame_timeout');assert.equal(r.joined,true);assert.equal(s.locked,false);});
test('decoder rejection and peer EOF are terminal, never decoder success',async()=>{const f=fixture();f.sink.accept=()=>{throw Error('private');};f.queue.enqueue(new ReadableStream({start(c){c.close();}}));assert.equal((await f.owner.closed).reason,'video_stream_failed');const g=fixture();g.queue.close();assert.equal((await g.owner.closed).reason,'video_streams_ended');});
test('late stream after stop is canceled without rendering',async()=>{const f=fixture();const s=new ReadableStream();f.queue.enqueue(s);const result=await f.owner.stop();assert.equal(result.joined,true);assert.equal(f.accepted.length,0);assert.equal(s.locked,false);});
test('reader or decoder release failure makes cleanup uncertain',async()=>{const f=fixture(),s=new ReadableStream({cancel(){throw Error('private');}});f.queue.enqueue(s);await tick();await assert.rejects(f.owner.stop(),{code:'video_cleanup_incomplete'});const g=fixture();g.sink.stats=()=>({decoderClosed:false,cleanupFailures:['decoder_close_failed']});await assert.rejects(g.owner.stop(),{code:'video_cleanup_incomplete'});});

test('throwing sink.close cannot strand closed or skip owned reader release',async()=>{const f=fixture(),s=new ReadableStream();f.queue.enqueue(s);await tick();f.sink.close=()=>{throw Error('private');};await assert.rejects(f.owner.stop(),{code:'video_cleanup_incomplete'});await assert.rejects(f.owner.closed,{code:'video_cleanup_incomplete'});assert.equal(s.locked,false);assert.equal(f.incoming.locked,false);assert.equal(f.owner.stats().active,0);});
test('initial incoming-reader failure retains failed decoder cleanup as uncertainty',()=>{const sink={close(){throw Error('private');},stats(){return {decoderClosed:false,cleanupFailures:[]};}};assert.throws(()=>receiveVideo({},sink),{code:'video_cleanup_incomplete',primaryCode:'video_streams_unavailable'});});


test('same-error terminal proof never accepts a different cancellation error',async()=>{
 const stored=Error('same text'),other=Error('same text'),terminal=observeStreamTerminal({closed:Promise.reject(stored)});await terminal.done;
 await assert.rejects(terminal.retire(()=>Promise.reject(other)),e=>e===other);await terminal.retire(()=>Promise.reject(stored));
});
test('future terminal error cannot retroactively excuse cancellation failure',async()=>{
 let reject;const stored=Error('fixed'),terminal=observeStreamTerminal({closed:new Promise((_,r)=>{reject=r;})});
 await assert.rejects(terminal.retire(()=>{reject(stored);return Promise.reject(stored);}),e=>e===stored);await terminal.done;
});
test('ordinary closed state and missing terminal observation cannot excuse cancellation failure',async()=>{
 const stored=Error('fixed'),terminal=observeStreamTerminal({closed:Promise.resolve()});await terminal.done;await assert.rejects(terminal.retire(()=>Promise.reject(stored)),e=>e===stored);
 assert.throws(()=>observeStreamTerminal({}),{code:'stream_terminal_unavailable'});
});
test('errored unclaimed frame releases its acquired lock without rendering',async()=>{
 const s=new ReadableStream({start(c){c.error(Error('fixed'));}});await retireUnclaimedReadable(s);assert.equal(s.locked,false);
});
test('errored media queue and active frame retain primary failure with joined cleanup',async()=>{
 const f=fixture();let frame;const stream=new ReadableStream({start(c){frame=c;}});f.queue.enqueue(stream);await tick();const error=Error('fixed');frame.error(error);f.queue.error(error);
 const result=await f.owner.closed;assert.equal(result.joined,true);assert.ok(['video_stream_failed','video_streams_failed'].includes(result.reason));assert.equal(f.accepted.length,0);assert.equal(stream.locked,false);assert.equal(f.incoming.locked,false);
});
