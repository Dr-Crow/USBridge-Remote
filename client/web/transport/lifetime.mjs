import {ProtocolError} from './wire.mjs';
// One immutable absolute deadline covers the whole setup. Individual stages
// cannot reset or extend it. Abort listeners/timer are released by dispose().
export class Lifetime {
  #controller=new AbortController(); #timer; #parent; #onAbort; #deadline; #clock;
  constructor({timeoutMs=5000,signal,clock=()=>performance.now()}={}) {
    if(!Number.isInteger(timeoutMs)||timeoutMs<1||timeoutMs>5000) throw new ProtocolError('setup_budget');
    this.#clock=clock;this.#deadline=clock()+timeoutMs;
    this.#timer=setTimeout(()=>this.cancel('setup_timeout'),timeoutMs);
    this.#parent=signal;this.#onAbort=()=>this.cancel('cancelled');
    if(signal?.aborted) this.#onAbort();else signal?.addEventListener('abort',this.#onAbort,{once:true});
  }
  get parentSignal(){return this.#parent;}
  get signal(){return this.#controller.signal;}
  check(){if(this.signal.aborted)throw this.signal.reason;if(this.#clock()>=this.#deadline){this.cancel('setup_timeout');throw this.signal.reason;}}
  cancel(code='cancelled'){if(!this.signal.aborted)this.#controller.abort(new ProtocolError(code));}
  async run(operation) {
    this.check();let listener;
    const aborted=new Promise((_,reject)=>{listener=()=>reject(this.signal.reason);this.signal.addEventListener('abort',listener,{once:true});});
    try {const result=await Promise.race([Promise.resolve().then(()=>{this.check();return operation(this.signal);}),aborted]);this.check();return result;}
    finally {this.signal.removeEventListener('abort',listener);}
  }
  async retire(operation) {
    // Cancellation does not grant a new cleanup budget. A late release is
    // reported as uncertain instead of turning an unfinished join into success.
    const remaining=this.#deadline-this.#clock();
    if(remaining<=0)throw new ProtocolError('cleanup_deadline');
    let timer;
    try{
      const result=await Promise.race([Promise.resolve().then(()=>{if(this.#clock()>=this.#deadline)throw new ProtocolError('cleanup_deadline');return operation();}),new Promise((_,reject)=>{timer=setTimeout(()=>reject(new ProtocolError('cleanup_deadline')),remaining);})]);
      if(this.#clock()>=this.#deadline)throw new ProtocolError('cleanup_deadline');
      return result;
    }
    finally{clearTimeout(timer);}
  }
  dispose(){clearTimeout(this.#timer);this.#parent?.removeEventListener('abort',this.#onAbort);}
}
// Release the readable lock and await cancellation on every failure. The owner
// must also close its transport so any pending browser I/O is retired.
export async function readBounded(readable,limit,lifetime) {
  if(!Number.isInteger(limit)||limit<1||limit>1048590)throw new ProtocolError('read_budget');
  const reader=readable.getReader();const parts=[];let count=0;let complete=false;let primary;
  const cancel=()=>{void reader.cancel().catch(()=>{});};lifetime.signal.addEventListener('abort',cancel,{once:true});
  try {
    for(;;){const {value,done}=await lifetime.run(()=>reader.read());if(done){complete=true;break;}
      if(!(value instanceof Uint8Array)||value.length>limit-count)throw new ProtocolError('read_size');
      count+=value.length;if(parts.length>=1024)throw new ProtocolError('read_fragment_count');parts.push(value);
    }
    const out=new Uint8Array(count);let at=0;for(const p of parts){out.set(p,at);at+=p.length;}return out;
  } catch(error) {primary=error instanceof ProtocolError?error:new ProtocolError('read_failed');throw primary;}
  finally {
    lifetime.signal.removeEventListener('abort',cancel);
    try{if(!complete)await lifetime.retire(()=>reader.cancel());}
    catch{const error=new ProtocolError('cleanup_deadline');error.primaryCode=primary?.code||'none';throw error;}
    finally{reader.releaseLock();}
  }
}
