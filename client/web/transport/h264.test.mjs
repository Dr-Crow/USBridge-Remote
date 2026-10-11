import test from 'node:test';import assert from 'node:assert/strict';
import {validateBaselineIDR,BASELINE_CODEC} from './h264.mjs';
const frames={blue:'AAAAAWdCwB7cIL+XARAAAAMAEAAAAwPA8WL4AAAAAWjODyyAAAAAAWWIhAS8RigACX3HAAFB6OAAJBsnJycnJycnXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXgA==',orange:'AAAAAWdCwB7cIL+XARAAAAMAEAAAAwPA8WL4AAAAAWjODyyAAAAAAWWIhAS8RigACWHHAAEqGOAAJtEnJycnJycnXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXgA=='};
const bytes=()=>new Uint8Array(Buffer.from(frames.blue,'base64'));
test('independent generated128x72 colors are baseline420 level3 all-IDR',()=>{for(const frame of Object.values(frames))assert.deepEqual(validateBaselineIDR(new Uint8Array(Buffer.from(frame,'base64')),128,72),{codec:BASELINE_CODEC,width:128,height:72,slices:1});});
test('profile, declared dimensions, invalid reference modes and non-IDR fail before decoder',()=>{
 for(const [at,value] of [[4,0xe7],[5,100],[6,0],[7,31]]){const b=bytes();b[at]=value;assert.throws(()=>validateBaselineIDR(b,128,72));}
 for(const size of [[126,72],[128,70],[1920,1080],[0,72],[127,72]])assert.throws(()=>validateBaselineIDR(bytes(),...size));
 const b=bytes();const index=b.findIndex((v,i)=>v===0x65&&b[i-1]===1);assert.ok(index>0);b[index]=0x41;assert.throws(()=>validateBaselineIDR(b,128,72));
});
test('truncated, unbounded, missing SPS/PPS and prefixed raw bytes fail closed',()=>{
 const b=bytes();for(let i=0;i<35;i++)assert.throws(()=>validateBaselineIDR(b.subarray(0,i),128,72));
 assert.throws(()=>validateBaselineIDR(b.subarray(27),128,72));assert.throws(()=>validateBaselineIDR(new Uint8Array(1048577),128,72));
 const extra=new Uint8Array(b.length+1);extra[0]=1;extra.set(b,1);assert.throws(()=>validateBaselineIDR(extra,128,72));
});
