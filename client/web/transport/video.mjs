import {ProtocolError,LatestIDR} from './wire.mjs';
import {Lifetime} from './lifetime.mjs';
import {BASELINE_CODEC,validateBaselineIDR} from './h264.mjs';
// First visible slice: owned canvas, bounded baseline420 all-IDR frames. No
// audio, input, device capture or transport selection is enabled by this sink.
export async function createCanvasDecoder({canvas,width,height,lifetime,onFailure=()=>{}},
  {Decoder=globalThis.VideoDecoder,Chunk=globalThis.EncodedVideoChunk}={}) {
  if(!(lifetime instanceof Lifetime)||typeof Decoder!=='function'||typeof Chunk!=='function'||typeof Decoder.isConfigSupported!=='function')throw new ProtocolError('video_unavailable');
  if(!Number.isInteger(width)||!Number.isInteger(height)||width<16||width>720||height<16||height>576||width%2||height%2||!canvas||typeof canvas.getContext!=='function')throw new ProtocolError('video_configuration');
  const config={codec:BASELINE_CODEC,codedWidth:width,codedHeight:height,optimizeForLatency:true,hardwareAcceleration:'prefer-software'};
  let supported;
  try {supported=await lifetime.run(()=>Decoder.isConfigSupported(config));}
  catch(error){throw error instanceof ProtocolError?error:new ProtocolError('video_support_failed');}
  if(!supported?.supported||supported.config?.codec!==BASELINE_CODEC||supported.config?.codedWidth!==width||supported.config?.codedHeight!==height)throw new ProtocolError('video_unsupported');
  lifetime.check();
  let context;
  try {context=canvas.getContext('2d',{alpha:false});canvas.width=width;canvas.height=height;}
  catch {throw new ProtocolError('canvas_unavailable');}
  if(!context)throw new ProtocolError('canvas_unavailable');
  const ordering=new LatestIDR(),pending=new Map();let decoder,closed=false,lastPresented=-1,terminalCode='closed';
  const cleanupFailures=[];
  const counts={received:0,dropped:0,decoded:0,presented:0,peakPending:0};
  function close(){if(closed)return;closed=true;pending.clear();try{decoder?.close();}catch{cleanupFailures.push('decoder_close_failed');}}
  function fail(code){if(closed)return;terminalCode=code;close();try{onFailure(new ProtocolError(code));}catch{terminalCode='video_failure_callback';}}
  try {
    decoder=new Decoder({
      error:()=>fail('video_decode_failed'),
      output:frame=>{
        try {
          if(closed)return;
          const timestamp=frame.timestamp;
          if(!Number.isSafeInteger(timestamp)||!pending.has(timestamp)||timestamp<=lastPresented||frame.displayWidth!==width||frame.displayHeight!==height){fail('video_output_invalid');return;}
          pending.delete(timestamp);counts.decoded++;lastPresented=timestamp;
          context.drawImage(frame,0,0,width,height);counts.presented++;
        } catch {fail('video_present_failed');}
        finally {try{frame.close();}catch{cleanupFailures.push('frame_close_failed');fail('video_frame_close_failed');}}
      }
    });
    decoder.configure(config);lifetime.check();
  } catch(error) {close();throw error instanceof ProtocolError?error:new ProtocolError('video_configuration_failed');}
  return Object.freeze({
    accept(packet){
      if(closed)throw new ProtocolError(terminalCode==='closed'?'video_closed':terminalCode);
      try {
        if(++counts.received>18000)throw new ProtocolError('video_frame_budget');
        const frame=ordering.accept(packet);
        if(frame===null){counts.dropped++;return false;}
        validateBaselineIDR(frame.data,width,height);
        if(frame.timestamp<=lastPresented||pending.has(frame.timestamp))throw new ProtocolError('video_timestamp_duplicate');
        if(pending.size>=2||decoder.decodeQueueSize>=2){counts.dropped++;return false;}
        pending.set(frame.timestamp,frame.sequence);counts.peakPending=Math.max(counts.peakPending,pending.size);
        decoder.decode(new Chunk({type:'key',timestamp:frame.timestamp,data:frame.data}));return true;
      } catch(error){const code=error instanceof ProtocolError?error.code:'video_decode_failed';fail(code);throw new ProtocolError(code);}
    },
    close,
    stats(){return Object.freeze({...counts,pending:pending.size,stopped:closed,decoderClosed:decoder?.state==='closed',cleanupFailures:[...cleanupFailures],terminalCode});}
  });
}
