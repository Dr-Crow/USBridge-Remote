import {PROTOCOL, ProtocolError, exactObject} from './wire.mjs';
// Static worker bootstrap only. Decoder/media execution remains unavailable
// until the versioned server and actual browser acceptance are implemented.
export function checkProbe(value) {
  exactObject(value,['type','protocol']);
  if(value.type!=='probe'||value.protocol!==PROTOCOL)throw new ProtocolError('worker_message');
  return Object.freeze({type:'ready',protocol:PROTOCOL,mediaEnabled:false});
}
if(typeof WorkerGlobalScope!=='undefined'&&globalThis instanceof WorkerGlobalScope) {
  let seen=false;
  globalThis.onmessage=event=>{
    try{if(seen)throw new ProtocolError('worker_message');seen=true;postMessage(checkProbe(event.data));}
    catch{postMessage({type:'failed',code:'worker_message'});close();}
  };
}
