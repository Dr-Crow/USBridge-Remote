import {ProtocolError,LIMITS} from './wire.mjs';
// Owned video-only receive lane. Invoked only after authenticated WELCOME.
// Four active streams, two decoder frames and one fixed per-frame read bound.
// The surrounding session owns the absolute authorization/retirement deadlines.
export function receiveVideo(transport,sink,{onFailure=()=>{},frameTimeoutMs=1000}={}) {
  if(!Number.isInteger(frameTimeoutMs)||frameTimeoutMs<1||frameTimeoutMs>1000)throw new ProtocolError('video_frame_budget');
  let incoming;
  try {incoming=transport.incomingUnidirectionalStreams.getReader();}
  catch {
    let clean=false;
    try{sink.close();const stats=sink.stats();clean=stats.decoderClosed&&!stats.cleanupFailures.length;}catch{}
    const error=new ProtocolError(clean?'video_streams_unavailable':'video_cleanup_incomplete');
    if(!clean)error.primaryCode='video_streams_unavailable';throw error;
  }
  const readers=new Set(),workers=new Set(),extraReleases=[];let stopping=false,failed=false,retirement,primary='closed',cleanupFailed=false,streamCount=0;
  let resolveClosed,rejectClosed;
  const closed=new Promise((resolve,reject)=>{resolveClosed=resolve;rejectClosed=reject;});void closed.catch(()=>{});
  function fail(code) {
    if(stopping||failed)return;
    failed=true;primary=code;
    queueMicrotask(()=>{try{onFailure(new ProtocolError(code));}catch{cleanupFailed=true;}void stop().catch(()=>{});});
  }
  async function frame(stream) {
    let reader,timer;
    try {
      reader=stream.getReader();readers.add(reader);
      timer=setTimeout(()=>fail('video_frame_timeout'),frameTimeoutMs);
      const chunks=[];let count=0;
      while(!stopping) {
        const item=await reader.read();if(stopping)break;
        if(item.done) {
          const packet=new Uint8Array(count);let at=0;for(const chunk of chunks){packet.set(chunk,at);at+=chunk.length;}
          sink.accept(packet);return;
        }
        if(!(item.value instanceof Uint8Array)||item.value.length>LIMITS.video+14-count||chunks.length>=1024)throw new ProtocolError('video_stream_size');
        count+=item.value.length;chunks.push(item.value);
      }
    } catch(error){if(!stopping)fail(error instanceof ProtocolError?error.code:'video_stream_failed');}
    finally {
      clearTimeout(timer);
      if(reader){
        // Cancellation is also awaited for a partially read rejected frame.
        // A browser that cannot settle this keeps closed pending and the
        // surrounding session reports its fixed cleanup deadline as uncertain.
        try{await reader.cancel();}catch{cleanupFailed=true;}
        try{reader.releaseLock();}catch{cleanupFailed=true;}
        readers.delete(reader);
      }
    }
  }
  const listener=(async()=>{
    try {
      while(!stopping) {
        const item=await incoming.read();
        if(item.done){if(!stopping)fail('video_streams_ended');break;}
        if(stopping){try{await item.value.cancel();}catch{cleanupFailed=true;}break;}
        if(++streamCount>18000||workers.size>=4){const release=Promise.resolve().then(()=>item.value.cancel());void release.catch(()=>{cleanupFailed=true;});extraReleases.push(release);fail('video_stream_budget');break;}
        const work=frame(item.value);workers.add(work);void work.then(()=>workers.delete(work),()=>{workers.delete(work);cleanupFailed=true;});
      }
    } catch {if(!stopping)fail('video_streams_failed');}
  })();
  function stop() {
    if(retirement)return retirement;
    stopping=true;try{sink.close();}catch{cleanupFailed=true;}
    retirement=(async()=>{
      const cancel=[Promise.resolve().then(()=>incoming.cancel()),...Array.from(readers,r=>Promise.resolve().then(()=>readers.has(r)?r.cancel():undefined))];
      const results=await Promise.allSettled(cancel);
      if(results.some(x=>x.status==='rejected'))cleanupFailed=true;
      await listener;await Promise.allSettled([...workers,...extraReleases]);
      try{incoming.releaseLock();}catch{cleanupFailed=true;}
      let stats;try{stats=sink.stats();}catch{throw new ProtocolError('video_cleanup_incomplete');}
      if(cleanupFailed||stats.cleanupFailures.length||!stats.decoderClosed||readers.size||workers.size)throw new ProtocolError('video_cleanup_incomplete');
      return Object.freeze({closed:true,joined:true,reason:primary,streams:streamCount,decoder:stats});
    })();
    void retirement.then(resolveClosed,rejectClosed);return retirement;
  }
  return Object.freeze({closed,stop,stats:()=>Object.freeze({streams:streamCount,active:workers.size,stopping,primary,decoder:sink.stats()})});
}
