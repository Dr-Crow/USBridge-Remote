// usbridge-webtransport-v1. Independent bounded browser-side implementation.
// This module performs no network, device, media or permission operation.
export const PROTOCOL = 'usbridge-webtransport-v1';
export const LIMITS = Object.freeze({control:4096, controlChunk:65536, messagesPerChunk:64, video:1048576, opus:1275, pendingFrames:4});
export class ProtocolError extends Error {
  constructor(code='protocol_invalid') { super(code); this.name='ProtocolError'; this.code=code; }
}
const fail = code => { throw new ProtocolError(code); };
const bytes = value => value instanceof Uint8Array ? value : fail('bytes_required');
const integer = (v, lo, hi) => Number.isSafeInteger(v) && v>=lo && v<=hi;
export function exactObject(value, keys) {
  if (!value || typeof value!=='object' || Array.isArray(value) || Object.keys(value).length!==keys.length || keys.some(k=>!Object.hasOwn(value,k))) fail('object_fields');
  return value;
}
export function canonicalJSON(raw) {
  bytes(raw); if (!raw.length || raw.length>LIMITS.control) fail('control_size');
  let text, value;
  try { text=new TextDecoder('utf-8',{fatal:true,ignoreBOM:true}).decode(raw); value=JSON.parse(text); }
  catch { fail('control_json'); }
  // Compact canonical JSON rejects duplicate keys, alternate number spellings,
  // ignored whitespace and ambiguous escapes without an eval/parser extension.
  if (JSON.stringify(value)!==text) fail('control_canonical');
  return value;
}
export function sessionID(value) {
  if (typeof value!=='string' || !/^[A-Za-z0-9_-]{1,64}$/.test(value)) fail('session_id');
  return value;
}
export function validateHello(raw) {
  const h=exactObject(canonicalJSON(raw),['protocol','session_id','ticket']);
  if(h.protocol!==PROTOCOL) fail('protocol_version'); sessionID(h.session_id);
  // 32 bytes unpadded base64url: final character carries exactly four bits.
  if(typeof h.ticket!=='string' || !/^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$/.test(h.ticket)) fail('ticket_encoding');
  return h;
}
export function validateWelcome(raw, expectedSession) {
  const w=exactObject(canonicalJSON(raw),['protocol','session_id','codec','width','height','fps','audio','input','channels']);
  if(w.protocol!==PROTOCOL || sessionID(w.session_id)!==expectedSession) fail('welcome_identity');
  if(w.codec!=='h264' || !integer(w.width,16,3840) || w.width%2 || !integer(w.height,16,2160) || w.height%2 || !integer(w.fps,1,60)) fail('welcome_video');
  const a=exactObject(w.audio,['codec','sample_rate','channels']);
  if(a.codec!=='opus' || a.sample_rate!==48000 || a.channels!==2 || w.input!==false || !Array.isArray(w.channels) || w.channels.length) fail('welcome_capability');
  return Object.freeze({...w,audio:Object.freeze({...a}),channels:Object.freeze([])});
}
export function encodeMessage(type,payload) {
  bytes(payload); if(!integer(type,1,255)||payload.length>LIMITS.control) fail('control_size');
  const out=new Uint8Array(5+payload.length);out[0]=type;new DataView(out.buffer).setUint32(1,payload.length);out.set(payload,5);return out;
}
export class ControlParser {
  #header=new Uint8Array(5); #headerUsed=0; #body=null; #used=0; #type=0; #allowed;
  constructor(allowed=[1,7]) { this.#allowed=new Set(allowed); }
  feed(chunk) {
    bytes(chunk); if(chunk.length>LIMITS.controlChunk) fail('control_chunk_size');
    const result=[];let at=0;
    while(at<chunk.length) {
      if(this.#headerUsed<5) {
        const n=Math.min(5-this.#headerUsed,chunk.length-at);this.#header.set(chunk.subarray(at,at+n),this.#headerUsed);this.#headerUsed+=n;at+=n;
        if(this.#headerUsed<5) break;
        this.#type=this.#header[0];const size=new DataView(this.#header.buffer).getUint32(1);
        if(!this.#allowed.has(this.#type)) fail('control_type');if(size>LIMITS.control) fail('control_size');
        this.#body=new Uint8Array(size);this.#used=0;
      }
      const n=Math.min(this.#body.length-this.#used,chunk.length-at);this.#body.set(chunk.subarray(at,at+n),this.#used);this.#used+=n;at+=n;
      if(this.#used===this.#body.length) {
        if(result.length>=LIMITS.messagesPerChunk) fail('control_message_count');
        result.push({type:this.#type,payload:this.#body});this.#headerUsed=0;this.#body=null;
      }
    }
    return result;
  }
  end() { if(this.#headerUsed || this.#body!==null) fail('control_truncated'); }
}
const timestamp = (view, offset) => {
  const n=view.getBigUint64(offset);if(n>BigInt(Number.MAX_SAFE_INTEGER)) fail('timestamp_range');return Number(n);
};
export function parseVideo(raw) {
  bytes(raw);if(raw.length<=14 || raw.length>LIMITS.video+14) fail('video_size');
  const d=new DataView(raw.buffer,raw.byteOffset,raw.byteLength);
  if(raw[0]!==1 || raw[1]!==1) fail('video_kind_or_flags');
  const sequence=d.getUint32(2);if(sequence===0) fail('video_sequence');
  return {sequence,timestamp:timestamp(d,6),key:true,data:raw.subarray(14)};
}
export function parseAudio(raw) {
  bytes(raw);if(raw.length<=9 || raw.length>LIMITS.opus+9 || raw[0]!==2) fail('audio_size_or_kind');
  return {timestamp:timestamp(new DataView(raw.buffer,raw.byteOffset,raw.byteLength),1),data:raw.subarray(9)};
}
// First v1 is all-IDR: a completed later frame can replace a missing earlier
// frame without retaining an unbounded reorder queue. Late/duplicate frames
// are discarded, never decoded backwards; sequence wrap is not permitted.
export class LatestIDR {
  #sequence=0; #timestamp=-1;
  accept(raw) {
    const f=parseVideo(raw);if(f.sequence<=this.#sequence) return null;
    if(f.timestamp<=this.#timestamp) fail('video_timestamp_order');
    this.#sequence=f.sequence;this.#timestamp=f.timestamp;return f;
  }
}
