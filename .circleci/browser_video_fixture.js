async () => {
  const {createCanvasDecoder}=await import('/transport/video.mjs');
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
  const lifetime=new Lifetime();let sink;const failures=[];
  let checked=0,changes=0,lastPixel;
  try {
    sink=await createCanvasDecoder({canvas,width:128,height:72,lifetime,onFailure:e=>failures.push(e.code)});
    const context=canvas.getContext('2d');
    for(let sequence=1;sequence<=12;sequence++) {
      lifetime.check();const color=(sequence-1)%2,unit=frames[color];
      const packet=new Uint8Array(unit.length+14),view=new DataView(packet.buffer);
      packet[0]=1;packet[1]=1;view.setUint32(2,sequence);view.setBigUint64(6,BigInt(sequence*33333));packet.set(unit,14);
      if(!sink.accept(packet))throw Error('fixture_frame_rejected');
      while(sink.stats().presented<sequence){lifetime.check();if(failures.length)throw Error('fixture_decode_failed');await lifetime.run(()=>new Promise(r=>setTimeout(r,5)));}
      await lifetime.run(()=>new Promise(requestAnimationFrame));
      const pixels=context.getImageData(0,0,128,72).data;
      for(let at=0;at<pixels.length;at+=4){for(let channel=0;channel<3;channel++)if(Math.abs(pixels[at+channel]-expected[color][channel])>10)throw Error('fixture_pixel_mismatch');if(pixels[at+3]!==255)throw Error('fixture_alpha_mismatch');checked++;}
      const pixel=Array.from(pixels.subarray(0,3));if(lastPixel&&pixel.some((v,i)=>Math.abs(v-lastPixel[i])>30))changes++;lastPixel=pixel;
    }
    const visible=canvas.getBoundingClientRect();if(visible.width!==128||visible.height!==72||getComputedStyle(canvas).visibility!=='visible')throw Error('fixture_canvas_hidden');
    sink.close();const stats=sink.stats();
    if(stats.presented!==12||stats.decoded!==12||stats.pending!==0||stats.dropped!==0||!stats.decoderClosed||stats.cleanupFailures.length||failures.length)throw Error('fixture_decoder_retirement');
    return {codec:'avc1.42C01E',width:128,height:72,canvas_visible:true,checked_pixels:checked,color_changes:changes,stats,real_webcodecs:true,real_quic:false,audio:false,input:false,capture:false};
  } finally {sink?.close();lifetime.dispose();canvas.remove();}
}
