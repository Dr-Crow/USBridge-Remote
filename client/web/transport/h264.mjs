import {ProtocolError,LIMITS} from './wire.mjs';
export const BASELINE_CODEC='avc1.42C01E';
const bad=()=>{throw new ProtocolError('h264_profile');};
class Bits {
  constructor(data){this.data=data;this.at=0;}
  bits(n){if(n<0||n>24||this.at+n>this.data.length*8)bad();let value=0;while(n--)value=value*2+((this.data[this.at>>3]>>(7-(this.at++&7)))&1);return value;}
  ue(limit){let zeros=0;while(this.bits(1)===0){if(++zeros>20)bad();}const value=2**zeros-1+this.bits(zeros);if(value>limit)bad();return value;}
}
function rbsp(nal,limit){
  if(nal.length<2||nal.length>limit)bad();const out=[];let zeros=0;
  for(let i=1;i<nal.length;i++){
    const byte=nal[i];
    if(zeros===2&&byte===3){if(i+1>=nal.length||nal[i+1]>3)bad();zeros=0;continue;}
    if(zeros===2&&byte<3)bad();out.push(byte);zeros=byte===0?zeros+1:0;
  }
  return new Uint8Array(out);
}
function start(data,from){for(let i=from;i+2<data.length;i++){if(data[i]===0&&data[i+1]===0){if(data[i+2]===1)return {at:i,size:3};if(data[i+2]===0&&data[i+3]===1)return {at:i,size:4};}}return null;}
function sps(nal,width,height){
  const b=new Bits(rbsp(nal,256));
  // First browser fixture is deliberately narrow:8-bit420 constrained
  // baseline, level3.0, progressive, at most one reference, no FMO.
  if(b.bits(8)!==66||b.bits(8)!==192||b.bits(8)!==30||b.ue(31)!==0)bad();
  b.ue(12);const poc=b.ue(2);if(poc===0)b.ue(12);else if(poc!==2)bad();
  b.ue(1);if(b.bits(1)!==0)bad();
  const codedWidth=(b.ue(44)+1)*16,codedHeight=(b.ue(35)+1)*16;
  if(codedWidth*codedHeight/256>1620||b.bits(1)!==1)bad();
  b.bits(1);let left=0,right=0,top=0,bottom=0;
  if(b.bits(1)){left=b.ue(360);right=b.ue(360);top=b.ue(288);bottom=b.ue(288);}
  if(left!==0||top!==0||codedWidth!==Math.ceil(width/16)*16||codedHeight!==Math.ceil(height/16)*16||codedWidth-2*right!==width||codedHeight-2*bottom!==height)bad();
}
export function validateBaselineIDR(data,width,height){
  if(!(data instanceof Uint8Array)||data.length<16||data.length>LIMITS.video||!Number.isInteger(width)||!Number.isInteger(height)||width<16||width>720||height<16||height>576||width%2||height%2)bad();
  let mark=start(data,0);if(!mark||mark.at!==0)bad();let count=0,seenSPS=false,seenPPS=false,slices=0,lastMB=-1;
  while(mark){if(++count>64)bad();const next=start(data,mark.at+mark.size);let end=next?next.at:data.length;while(end>mark.at+mark.size&&data[end-1]===0)end--;
    const nal=data.subarray(mark.at+mark.size,end);if(nal.length<2||nal[0]&128)bad();const type=nal[0]&31;
    if(type===7){if(seenSPS||seenPPS||slices||!(nal[0]&96))bad();sps(nal,width,height);seenSPS=true;}
    else if(type===8){if(!seenSPS||seenPPS||slices||!(nal[0]&96))bad();const b=new Bits(rbsp(nal,256));if(b.ue(255)!==0||b.ue(31)!==0||b.bits(1)!==0||b.bits(1)!==0||b.ue(7)!==0)bad();seenPPS=true;}
    else if(type===5){if(!seenSPS||!seenPPS||!(nal[0]&96)||++slices>16)bad();const b=new Bits(rbsp(nal.subarray(0,Math.min(nal.length,64)),64));const mb=b.ue(Math.ceil(width/16)*Math.ceil(height/16)-1);if((slices===1&&mb!==0)||mb<=lastMB)bad();lastMB=mb;const slice=b.ue(9);if((slice!==2&&slice!==7)||b.ue(255)!==0)bad();}
    else if((type!==6&&type!==9)||(nal[0]&96))bad();
    mark=next;
  }
  if(!seenSPS||!seenPPS||!slices)bad();return Object.freeze({codec:BASELINE_CODEC,width,height,slices});
}
