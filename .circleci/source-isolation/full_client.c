/* Acceptance driver for unchanged moonlight-common-c. Session key arrives on stdin. */
#include "Limelight.h"
#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <stdatomic.h>
#include <unistd.h>
#include <pthread.h>
#include <stdarg.h>
#include <arpa/inet.h>
typedef struct OpusMSDecoder OpusMSDecoder;
extern OpusMSDecoder *opus_multistream_decoder_create(int,int,int,int,const unsigned char*,int*);
extern int opus_multistream_decode(OpusMSDecoder*,const unsigned char*,int,short*,int,int);
extern void opus_multistream_decoder_destroy(OpusMSDecoder*);
static FILE *video;
static OpusMSDecoder *decoder;
static atomic_int frames, samples, audio_errors, terminated, finished, stage_error, input_errors;
static void checked_input(int result){if(result)atomic_fetch_add(&input_errors,1);}
static int early_termination;
static atomic_int log_bytes;
static void diagnostic(const char *fmt,...){char b[2048];va_list a;va_start(a,fmt);vsnprintf(b,sizeof(b),fmt,a);va_end(a);if(strstr(b,"key")||strstr(b,"Key")||strstr(b,"secret")||strstr(b,"token"))return;if(atomic_fetch_add(&log_bytes,(int)strlen(b))<16384)fputs(b,stderr);}
static int setup(int f,int w,int h,int rate,void*c,int flags){return (f==VIDEO_FORMAT_H264||f==VIDEO_FORMAT_H264_HIGH8_444)?0:-1;}
static int submit(PDECODE_UNIT u){for(PLENTRY p=u->bufferList;p;p=p->next)if(fwrite(p->data,1,p->length,video)!=(size_t)p->length)return DR_NEED_IDR;atomic_fetch_add(&frames,1);return DR_OK;}
static int audio_init(int a,const POPUS_MULTISTREAM_CONFIGURATION c,void*x,int flags){int e=0;decoder=opus_multistream_decoder_create(c->sampleRate,c->channelCount,c->streams,c->coupledStreams,c->mapping,&e);return decoder?0:e;}
static void audio_sample(char*d,int n){short pcm[5760*8];int k=opus_multistream_decode(decoder,(unsigned char*)d,n,pcm,5760,0);if(k<0)atomic_fetch_add(&audio_errors,1);else atomic_fetch_add(&samples,k);}
static void audio_cleanup(void){if(decoder)opus_multistream_decoder_destroy(decoder);decoder=NULL;}
static void stage_fail(int s,int e){atomic_store(&stage_error,e?e:-1);fprintf(stderr,"stage_failed=%s error=%d\n",LiGetStageName(s),e);}
static void stage_done(int s){fprintf(stderr,"stage_complete=%s\n",LiGetStageName(s));}
static void ended(int e){atomic_store(&terminated,1);fprintf(stderr,"connection_terminated=%d\n",e);}
static void *watchdog(void*p){for(int i=0;i<300&&!atomic_load(&finished);i++)usleep(100000);if(!atomic_load(&finished))LiInterruptConnection();return NULL;}
int main(int argc,char**argv){
 if(argc<3 || argc>5){fprintf(stderr,"usage: full_client RTSP_URL H264_OUTPUT (20 secret bytes on stdin)\n");return 2;}
 int inject_input=0, chroma444=0;
 for(int i=3;i<argc;i++){if(strcmp(argv[i],"input")==0)inject_input=1;else if(strcmp(argv[i],"444")==0)chroma444=1;else return 2;}
 unsigned char secret[20];if(fread(secret,1,20,stdin)!=20)return 2;
 video=fopen(argv[2],"wb");if(!video)return 2;
 SERVER_INFORMATION s; STREAM_CONFIGURATION c; DECODER_RENDERER_CALLBACKS v; AUDIO_RENDERER_CALLBACKS a; CONNECTION_LISTENER_CALLBACKS l;
 LiInitializeServerInformation(&s);LiInitializeStreamConfiguration(&c);LiInitializeVideoCallbacks(&v);LiInitializeAudioCallbacks(&a);LiInitializeConnectionCallbacks(&l);
 s.address="127.0.0.1";s.serverInfoAppVersion="7.1.431.-1";s.rtspSessionUrl=argv[1];s.serverCodecModeSupport=SCM_H264;if(chroma444)s.serverCodecModeSupport|=SCM_H264_HIGH8_444;
 c.width=128;c.height=72;c.fps=30;c.bitrate=1000;c.packetSize=1056;c.streamingRemotely=STREAM_CFG_LOCAL;c.audioConfiguration=AUDIO_CONFIGURATION_STEREO;c.supportedVideoFormats=VIDEO_FORMAT_H264;c.encryptionFlags=ENCFLG_ALL;if(chroma444)c.supportedVideoFormats=VIDEO_FORMAT_H264_HIGH8_444;
 memcpy(c.remoteInputAesKey,secret,16);memcpy(c.remoteInputAesIv,secret+16,4);memset(secret,0,sizeof(secret));
 v.setup=setup;v.submitDecodeUnit=submit;v.capabilities=CAPABILITY_DIRECT_SUBMIT;
 a.init=audio_init;a.decodeAndPlaySample=audio_sample;a.cleanup=audio_cleanup;
 l.logMessage=diagnostic;l.stageFailed=stage_fail;l.stageComplete=stage_done;l.connectionTerminated=ended;
 pthread_t guard;pthread_create(&guard,NULL,watchdog,NULL);
 int rc=LiStartConnection(&s,&c,&l,&v,&a,NULL,0,NULL,0);
 if(rc==0){
  for(int i=0;i<50&&!atomic_load(&terminated);i++){
   if(inject_input&&i==5){
    checked_input(LiSendMousePositionEvent(64,36,128,72));
    checked_input(LiSendMouseMoveEvent(4,3));
    checked_input(LiSendKeyboardEvent((short)0x80a3,KEY_ACTION_DOWN,2));
    checked_input(LiSendKeyboardEvent((short)0x8041,KEY_ACTION_DOWN,2));
    checked_input(LiSendKeyboardEvent((short)0x8041,KEY_ACTION_UP,2));
    checked_input(LiSendKeyboardEvent((short)0x80a3,KEY_ACTION_UP,0));
    checked_input(LiSendHighResScrollEvent(120));
    checked_input(LiSendHighResHScrollEvent(120));
   }
   // Leave two inputs held to verify release on encrypted peer disconnect.
   if(inject_input&&i==40){
    checked_input(LiSendKeyboardEvent((short)0x8041,KEY_ACTION_DOWN,0));
    checked_input(LiSendMouseButtonEvent(BUTTON_ACTION_PRESS,BUTTON_LEFT));
   }
   usleep(100000);
  }
  early_termination=atomic_load(&terminated);LiStopConnection();
 }
 atomic_store(&finished,1);pthread_join(guard,NULL);fclose(video);
 int ok=rc==0&&!early_termination&&atomic_load(&frames)>=30&&atomic_load(&samples)>=48000&&atomic_load(&audio_errors)==0&&atomic_load(&input_errors)==0;
 printf("{\"passed\":%s,\"start_result\":%d,\"video_decode_units\":%d,\"decoded_audio_samples\":%d,\"audio_errors\":%d,\"stage_error\":%d}\n",ok?"true":"false",rc,atomic_load(&frames),atomic_load(&samples),atomic_load(&audio_errors),atomic_load(&stage_error));
 return ok?0:1;
}
