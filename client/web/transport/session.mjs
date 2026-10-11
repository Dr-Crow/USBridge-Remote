import {ProtocolError,ControlParser,encodeMessage,validateHello,validateWelcome,exactObject} from './wire.mjs';
import {discovery,endpoint} from './policy.mjs';
import {Lifetime} from './lifetime.mjs';
const enc=new TextEncoder();
const fail=code=>new ProtocolError(code);
// Control-only owner; not exposed by the page's disabled API. The supplied
// grant must already be authorized. Server-side redemption must be atomic.
export async function negotiateControl({grant,authorizedEndpoint,capabilityStatus,capabilityBody,signal,lifetime},
  {Transport=globalThis.WebTransport,now=()=>Date.now(),monotonic=()=>performance.now(),pageOrigin=globalThis.location?.origin,cleanupTimeoutMs=5000}={}) {
  if(!Number.isInteger(cleanupTimeoutMs)||cleanupTimeoutMs<1||cleanupTimeoutMs>5000)throw fail('cleanup_budget');
  if(lifetime!==undefined&&!(lifetime instanceof Lifetime))throw fail('setup_lifetime');
  exactObject(grant,['protocol','session_id','ticket','endpoint','origin','expires_at_ms','width','height','fps']);
  endpoint(grant.endpoint,authorizedEndpoint);
  const time=now(),started=monotonic(),expiresAt=grant.expires_at_ms;
  if(typeof pageOrigin!=='string'||grant.origin!==pageOrigin||!Number.isSafeInteger(expiresAt)||expiresAt<=time||expiresAt-time>300000)throw fail('grant_scope');
  const candidate=discovery(capabilityStatus,capabilityBody,authorizedEndpoint);
  if(!candidate.available)throw fail(candidate.reason);
  if(typeof Transport!=='function')throw fail('transport_unavailable');
  // Validate all grant fields before starting any I/O. These are the same
  // video bounds required of the WELCOME, not merely a later comparison.
  const expected=validateWelcome(enc.encode(JSON.stringify({protocol:grant.protocol,session_id:grant.session_id,codec:'h264',width:grant.width,height:grant.height,fps:grant.fps,audio:{codec:'opus',sample_rate:48000,channels:2},input:false,channels:[]})),grant.session_id);
  const hello=enc.encode(JSON.stringify({protocol:grant.protocol,session_id:grant.session_id,ticket:grant.ticket}));
  try{validateHello(hello);}catch(error){hello.fill(0);throw error;}
  const ownsLifetime=lifetime===undefined,setup=lifetime||new Lifetime({signal});
  const parentSignals=[...new Set([signal,setup.parentSignal].filter(Boolean))];
  let transport,reader,writer,leaseTimer,closing=false,established=false,retirement,helloWrite,lateReleaseFailed=false;
  const active=new Set();
  const track=p=>{const task=Promise.resolve(p);active.add(task);void task.then(()=>active.delete(task),()=>active.delete(task));return task;};
  const parser=new ControlParser([1,7]);let welcome;
  const checkLive=()=>{setup.check();if(closing||parentSignals.some(s=>s.aborted))throw fail('cancelled');if(now()>=expiresAt||monotonic()-started>=expiresAt-time)throw fail('grant_expired');};
  let resolveClosed,rejectClosed;
  const closed=new Promise((resolve,reject)=>{resolveClosed=resolve;rejectClosed=reject;});
  // Background failure is retained on the public closed promise, even if its
  // observer is attached later. Never log browser errors or request bytes.
  void closed.catch(()=>{});
  function retire(reason) {
    if(retirement)return retirement;
    closing=true;clearTimeout(leaseTimer);
    for(const parent of parentSignals)parent.removeEventListener('abort',leaseAbort);
    setup.signal.removeEventListener('abort',setupAbort);
    retirement=(async()=>{
      let uncertain=false;
      try{transport?.close({closeCode:0,reason:'closed'});}catch{uncertain=true;}
      const releases=[];
      if(reader)releases.push(Promise.resolve().then(()=>reader.cancel()));
      if(writer)releases.push(Promise.resolve().then(()=>writer.abort()));
      // Setup failure retains the original absolute deadline. Once established,
      // an independent, non-renewable stop budget bounds the session's cleanup.
      const stopBudget=established?new Lifetime({timeoutMs:cleanupTimeoutMs}):null;
      try {
        // Attach rejection handlers immediately, even when the deadline has
        // already expired and therefore refuses to invoke a join operation.
        const joined=Promise.allSettled([...releases,...active]);
        const results=await (stopBudget?stopBudget.run(()=>joined):setup.retire(()=>joined));
        if(results.slice(0,releases.length).some(x=>x.status==='rejected'))uncertain=true;
      } catch {uncertain=true;}
      finally {
        stopBudget?.dispose();
        try{reader?.releaseLock();}catch{uncertain=true;}
        try{writer?.releaseLock();}catch{uncertain=true;}
      }
      if(!helloWrite||!active.has(helloWrite))hello.fill(0);
      if(lateReleaseFailed)uncertain=true;
      if(uncertain){const error=fail('session_cleanup_incomplete');error.primaryCode=reason;throw error;}
      return Object.freeze({closed:true,joined:true,reason});
    })();
    void retirement.then(resolveClosed,rejectClosed);
    return retirement;
  }
  function backgroundClose(reason){void retire(reason).catch(()=>{});}
  const setupAbort=()=>backgroundClose(setup.signal.reason?.code||'cancelled');
  const leaseAbort=()=>backgroundClose('cancelled');
  setup.signal.addEventListener('abort',setupAbort,{once:true});
  for(const parent of parentSignals)parent.addEventListener('abort',leaseAbort,{once:true});
  try {
    checkLive();
    transport=new Transport(candidate.endpoint,{congestionControl:'low-latency'});
    const ended=track(transport.closed);
    void ended.then(()=>backgroundClose('peer_closed'),()=>backgroundClose('transport_failed'));
    await setup.run(()=>track(transport.ready));checkLive();
    const acquire=track(Promise.resolve().then(()=>{checkLive();return transport.createBidirectionalStream();}).then(async stream=>{
      if(closing){const result=await Promise.allSettled([stream.readable.cancel(),stream.writable.abort()]);if(result.some(x=>x.status==='rejected')){lateReleaseFailed=true;throw fail('late_stream_release_failed');}throw fail('cancelled');}
      reader=stream.readable.getReader();writer=stream.writable.getWriter();
    }));
    await setup.run(()=>acquire);checkLive();
    const frame=encodeMessage(1,hello);
    helloWrite=track(Promise.resolve().then(()=>{checkLive();return writer.write(frame);}));
    void helloWrite.finally(()=>{frame.fill(0);hello.fill(0);}).catch(()=>{});
    await setup.run(()=>helloWrite);checkLive();
    while(!welcome) {
      const item=await setup.run(()=>{checkLive();return track(reader.read());});checkLive();
      if(item.done){parser.end();throw fail('welcome_missing');}
      for(const message of parser.feed(item.value)) {
        if(message.type!==1||welcome)throw fail('welcome_protocol');
        welcome=validateWelcome(message.payload,expected.session_id);
        if(welcome.width!==expected.width||welcome.height!==expected.height||welcome.fps!==expected.fps)throw fail('welcome_grant_mismatch');
      }
    }
    setup.check();if(closing||parentSignals.some(s=>s.aborted))throw fail('cancelled');
    const leaseRemaining=Math.min(expiresAt-now(),expiresAt-time-(monotonic()-started));
    if(leaseRemaining<=0)throw fail('grant_expired');
    established=true;
    setup.signal.removeEventListener('abort',setupAbort);
    // The parent signal remains live until retirement, not just until ready.
    const monitor=track((async()=>{
      try {
        while(!closing){const item=await reader.read();if(item.done){parser.end();break;}
          for(const message of parser.feed(item.value)){if(message.type!==7||message.payload.length!==0)throw fail('control_protocol');queueMicrotask(()=>backgroundClose('taken_over'));return;}
        }
      } catch {if(!closing)queueMicrotask(()=>backgroundClose('control_failed'));return;}
      if(!closing)queueMicrotask(()=>backgroundClose('peer_closed'));
    })());
    void monitor;
    leaseTimer=setTimeout(()=>backgroundClose('expired'),leaseRemaining);
    return Object.freeze({welcome,close:()=>retire('closed'),closed,mediaStarted:false});
  } catch(error) {
    const primary=error instanceof ProtocolError?error:fail('transport_failed');
    try{await retire(primary.code);}catch(cleanup){cleanup.primaryCode=primary.code;throw cleanup;}
    throw primary;
  } finally {
    if(ownsLifetime)setup.dispose();
  }
}
