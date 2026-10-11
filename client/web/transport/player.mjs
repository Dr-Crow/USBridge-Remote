import {PROTOCOL, ProtocolError} from './wire.mjs';
import {Lifetime} from './lifetime.mjs';
// This first vertical slice loads static assets and validates their readiness.
// No server negotiation/streaming selection is enabled by loading this module.
export async function probeWorker({signal,lifetime}={}) {
  if(lifetime!==undefined&&(!(lifetime instanceof Lifetime)||signal!==undefined))throw new ProtocolError('setup_lifetime');
  const ownsLifetime=lifetime===undefined;
  const life=lifetime||new Lifetime({signal});let worker;
  try {
    life.check();worker=new Worker(new URL('./worker.mjs',import.meta.url),{type:'module'});
    return await life.run(()=>new Promise((resolve,reject)=>{
      worker.onerror=()=>reject(new ProtocolError('worker_startup'));
      worker.onmessage=({data})=>{
        if(!data||Object.keys(data).length!==3||data.type!=='ready'||data.protocol!==PROTOCOL||data.mediaEnabled!==false){reject(new ProtocolError('worker_protocol'));return;}
        resolve(Object.freeze({protocol:PROTOCOL,staticWorkerReady:true,mediaEnabled:false}));
      };
      worker.postMessage({type:'probe',protocol:PROTOCOL});
    }));
  } finally {if(worker){worker.onmessage=null;worker.onerror=null;worker.terminate();}if(ownsLifetime)life.dispose();}
}
export const API=Object.freeze({protocol:PROTOCOL,stage:'contract-foundation',transportEnabled:false,supported:()=>false,probeWorker});
const existing=Object.getOwnPropertyDescriptor(globalThis,'__usbridgeSecureWT');
if(existing)throw new ProtocolError('player_identity');
Object.defineProperty(globalThis,'__usbridgeSecureWT',{value:API,writable:false,configurable:false,enumerable:false});
