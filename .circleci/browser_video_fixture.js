async () => {
  const {negotiateVideo}=await import('/transport/session.mjs');
  const {PROTOCOL,ControlParser,encodeMessage,validateHello}=await import('/transport/wire.mjs');
  const {Lifetime}=await import('/transport/lifetime.mjs');
  // Independent synthetic IDR units: official Debian FFmpeg7.1.5/libx264
  // baseline420 level3, optional SEI removed. No capture/private component.
  const frames=[
    'AAAAAWdCwB7cIL+XARAAAAMAEAAAAwPA8WL4AAAAAWjODyyAAAAAAWWIhAS8RigACX3HAAFB6OAAJBsnJycnJycnXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXgA==',
    'AAAAAWdCwB7cIL+XARAAAAMAEAAAAwPA8WL4AAAAAWjODyyAAAAAAWWIhAS8RigACWHHAAEqGOAAJtEnJycnJycnXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXgA=='
  ].map(s=>Uint8Array.from(atob(s),c=>c.charCodeAt(0)));
  const expected=[[49,99,219],[180,75,22]];
  const canvas=document.createElement('canvas');canvas.id='owned-video-fixture';
  canvas.style.position='fixed';canvas.style.top='0';canvas.style.left='0';
  canvas.style.width='128px';canvas.style.height='72px';canvas.style.zIndex='2147483647';
  document.body.appendChild(canvas);
  const lifetime=new Lifetime();let owner;const failures=[];
  const json=x=>new TextEncoder().encode(JSON.stringify(x));let control,video,ended,closes=0,helloVerified=false;
  const closed=new Promise(resolve=>{ended=resolve;});
  const welcome={protocol:PROTOCOL,session_id:'canvas_fixture',codec:'h264',width:128,height:72,fps:30,audio:{codec:'opus',sample_rate:48000,channels:2},input:false,channels:[]};
  const readable=new ReadableStream({start(c){control=c;}});
  const writable=new WritableStream({write(frame){const [message]=new ControlParser([1]).feed(frame);helloVerified=validateHello(message.payload).session_id===welcome.session_id;control.enqueue(encodeMessage(1,json(welcome)));}});
  const incoming=new ReadableStream({start(c){video=c;}}),endpoint='https://127.0.0.1:8444/webtransport';
  class MockTransport{constructor(){this.ready=Promise.resolve();this.closed=closed;this.incomingUnidirectionalStreams=incoming;}createBidirectionalStream(){return Promise.resolve({readable,writable});}close(){closes++;ended();}}
  let checked=0,changes=0,lastPixel;
  try {
    owner=await negotiateVideo({canvas,lifetime,grant:{protocol:PROTOCOL,session_id:welcome.session_id,ticket:'A'.repeat(43),endpoint,origin:location.origin,ticket_expires_at_ms:Date.now()+10000,session_expires_at_ms:Date.now()+10000,width:128,height:72,fps:30},authorizedEndpoint:endpoint,capabilityStatus:200,capabilityBody:json({protocol:PROTOCOL,ready:true,endpoint,tls_mode:'trusted',codec:'h264',audio:'opus',input:false,channels:[]})},{Transport:MockTransport});
    void owner.closed.catch(e=>failures.push(e.code));
    const context=canvas.getContext('2d');
    for(let sequence=1;sequence<=12;sequence++) {
      lifetime.check();const color=(sequence-1)%2,unit=frames[color];
      const packet=new Uint8Array(unit.length+14),view=new DataView(packet.buffer);
      packet[0]=1;packet[1]=1;view.setUint32(2,sequence);view.setBigUint64(6,BigInt(sequence*33333));packet.set(unit,14);
      video.enqueue(new ReadableStream({start(c){c.enqueue(packet.subarray(0,7));c.enqueue(packet.subarray(7));c.close();}}));
      while(owner.videoStats().decoder.presented<sequence){lifetime.check();if(failures.length)throw Error('fixture_decode_failed');await lifetime.run(()=>new Promise(r=>setTimeout(r,5)));}
      await lifetime.run(()=>new Promise(requestAnimationFrame));
      const pixels=context.getImageData(0,0,128,72).data;
      for(let at=0;at<pixels.length;at+=4){for(let channel=0;channel<3;channel++)if(Math.abs(pixels[at+channel]-expected[color][channel])>10)throw Error('fixture_pixel_mismatch');if(pixels[at+3]!==255)throw Error('fixture_alpha_mismatch');checked++;}
      const pixel=Array.from(pixels.subarray(0,3));if(lastPixel&&pixel.some((v,i)=>Math.abs(v-lastPixel[i])>30))changes++;lastPixel=pixel;
    }
    const visible=canvas.getBoundingClientRect();if(visible.width!==128||visible.height!==72||getComputedStyle(canvas).visibility!=='visible')throw Error('fixture_canvas_hidden');
    const stopped=await owner.close(),stats=stopped.video.decoder;
    if(!stopped.joined||stopped.video.active||closes!==1||!helloVerified||incoming.locked||readable.locked||writable.locked)throw Error('fixture_session_retirement');
    if(stats.presented!==12||stats.decoded!==12||stats.pending!==0||stats.dropped!==0||!stats.decoderClosed||stats.cleanupFailures.length||failures.length)throw Error('fixture_decoder_retirement');
    return {codec:'avc1.42C01E',width:128,height:72,canvas_visible:true,checked_pixels:checked,color_changes:changes,stats,real_webcodecs:true,owned_stream_receiver:true,session_joined:true,real_quic:false,audio:false,input:false,capture:false};
  } finally {if(owner)await owner.close();lifetime.dispose();canvas.remove();}
}
