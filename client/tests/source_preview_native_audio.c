// Callback-level native regression test. Uses the actual shared audio callbacks
// and Opus decoder, with counted output-backend functions. No capture or server
// session is started. Link against the pinned Moonlight/ENet and system Opus.
#include <assert.h>
#include <stdio.h>
#include <Limelight.h>
#include <opus_multistream.h>

void goMoonlightStage(int stage, int result, int errCode);
void goMoonlightConnected(void);
void goMoonlightTerminated(int errCode);
void goMoonlightRumble(unsigned short controllerNumber, unsigned short lowFreq, unsigned short highFreq);
void goVideoFormatNegotiated(int videoFormat);
void goVTLog(char *message);

#include "../internal/service/moonlight_cgo_shared.h"

static int output_opens, output_writes, output_closes;
void platform_ar_init(int channels, int sample_rate) { (void)channels; (void)sample_rate; output_opens++; }
void platform_ar_cleanup(void) { output_closes++; }
void platform_ar_decode(const opus_int16 *pcm, int byte_count, int samples) {
    assert(pcm != NULL && byte_count > 0 && samples > 0);
    output_writes++;
}
int platform_dr_submit(PDECODE_UNIT du) { (void)du; return DR_OK; }
void platform_post_stop(void) {}
void platform_set_video_format(int format) { (void)format; }
void bench_frames_note(const DECODE_UNIT *du) { (void)du; }
void goMoonlightStage(int stage, int result, int errCode) { (void)stage; (void)result; (void)errCode; }
void goMoonlightConnected(void) {}
void goMoonlightTerminated(int errCode) { (void)errCode; }
void goMoonlightRumble(unsigned short n, unsigned short low, unsigned short high) { (void)n; (void)low; (void)high; }
void goVideoFormatNegotiated(int format) { (void)format; }
void goVTLog(char *message) { (void)message; }

static void exercise_audio_callbacks(int discard) {
    OPUS_MULTISTREAM_CONFIGURATION cfg = {0};
    cfg.sampleRate = 48000;
    cfg.channelCount = 2;
    cfg.streams = 1;
    cfg.coupledStreams = 1;
    cfg.samplesPerFrame = 240;
    cfg.mapping[0] = 0;
    cfg.mapping[1] = 1;
    output_opens = output_writes = output_closes = 0;
    g_audio_discard = discard;
    assert(ar_init(AUDIO_CONFIGURATION_STEREO, &cfg, NULL, 0) == 0);
    assert(g_opus_ms_decoder != NULL);
    ar_decode(NULL, 0); // Real Opus PLC creates decoded silent PCM.
    assert(g_ar_plc_count == 1 && g_ar_err_count == 0);
    ar_cleanup();
    assert(g_opus_ms_decoder == NULL);
    if (discard) {
        assert(output_opens == 0 && output_writes == 0 && output_closes == 0);
    } else {
        assert(output_opens == 1 && output_writes == 1 && output_closes == 1);
    }
}

int main(void) {
    exercise_audio_callbacks(1); // Source preview: consume/decode/discard.
    exercise_audio_callbacks(0); // Stock: preserve normal output lifecycle.
    puts("source preview silent audio and stock output callback checks passed");
    return 0;
}
