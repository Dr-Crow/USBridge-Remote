//go:build linux && !android && cgo

// V4L2 camera capture encoded to H.264 for the stream's camera uplink (see
// uplink_camera_linux.go). MJPEG is preferred (a USB 2.0 webcam only does
// 720p30 compressed), YUYV is the fallback; the picture is scaled down to
// at most 1280x720 and encoded with the first H.264 encoder FFmpeg has --
// libx264 normally -- tuned for latency: no B-frames, SPS/PPS with every
// keyframe.

#include "uplink_camera_linux.h"

#include <errno.h>
#include <fcntl.h>
#include <poll.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <unistd.h>

#include <linux/videodev2.h>

#include <libavcodec/avcodec.h>
#include <libavutil/imgutils.h>
#include <libavutil/opt.h>
#include <libswscale/swscale.h>

#define CAM_BUFFERS 4
#define CAM_MAX_W 1280
#define CAM_MAX_H 720
#define CAM_FPS 30

struct cam {
    int fd;
    uint32_t fourcc;
    int w, h;
    void *buf[CAM_BUFFERS];
    size_t len[CAM_BUFFERS];
    int nbuf;
    int streaming;

    AVCodecContext *dec;
    AVPacket *dpkt;
    AVFrame *dframe;

    AVCodecContext *enc;
    AVPacket *epkt;
    AVFrame *eframe;
    struct SwsContext *sws;
    int64_t pts;

    uint8_t *out;
    int outcap;
};

static void seterr(char *err, int errlen, const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(err, errlen, fmt, ap);
    va_end(ap);
}

// Webcams' MJPEG often has APP segments FFmpeg can't parse, and it says so
// at error level on every frame. The client decodes MJPEG nowhere else, so
// that decoder's messages are dropped; everything else goes on as usual.
static void quiet_mjpeg_log(void *avcl, int level, const char *fmt, va_list vl) {
    if (avcl && level > AV_LOG_FATAL) {
        const AVClass *cls = *(const AVClass **)avcl;
        if (cls && !strcmp(cls->class_name, "AVCodecContext")) {
            const AVCodecContext *ctx = avcl;
            if (ctx->codec_id == AV_CODEC_ID_MJPEG && ctx->codec && av_codec_is_decoder(ctx->codec)) {
                return;
            }
        }
    }
    av_log_default_callback(avcl, level, fmt, vl);
}

static pthread_once_t quiet_once = PTHREAD_ONCE_INIT;

static void install_quiet_log(void) {
    av_log_set_callback(quiet_mjpeg_log);
}

static int xioctl(int fd, unsigned long req, void *arg) {
    int r;
    do {
        r = ioctl(fd, req, arg);
    } while (r < 0 && errno == EINTR);
    return r;
}

int cam_is_capture_device(const char *path) {
    struct v4l2_capability cap;
    int fd = open(path, O_RDWR | O_NONBLOCK);
    if (fd < 0) {
        return 0;
    }
    int ok = 0;
    if (xioctl(fd, VIDIOC_QUERYCAP, &cap) == 0) {
        uint32_t caps = (cap.capabilities & V4L2_CAP_DEVICE_CAPS) ? cap.device_caps : cap.capabilities;
        ok = (caps & V4L2_CAP_VIDEO_CAPTURE) && (caps & V4L2_CAP_STREAMING);
    }
    close(fd);
    return ok;
}

static int has_format(int fd, uint32_t fourcc) {
    struct v4l2_fmtdesc fd_desc;
    memset(&fd_desc, 0, sizeof(fd_desc));
    fd_desc.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    for (fd_desc.index = 0; xioctl(fd, VIDIOC_ENUM_FMT, &fd_desc) == 0; fd_desc.index++) {
        if (fd_desc.pixelformat == fourcc) {
            return 1;
        }
    }
    return 0;
}

static AVCodecContext *open_encoder(int w, int h, int bitrate, char *err, int errlen) {
    // Hardware encoders that take frames from system memory, then software.
    static const char *names[] = { "libx264", "h264_nvenc", "h264_qsv", "h264_amf", "libopenh264", NULL };
    for (int i = 0; names[i]; i++) {
        const AVCodec *codec = avcodec_find_encoder_by_name(names[i]);
        if (!codec) {
            continue;
        }
        AVCodecContext *c = avcodec_alloc_context3(codec);
        if (!c) {
            continue;
        }
        c->width = w;
        c->height = h;
        c->time_base = (AVRational){ 1, CAM_FPS };
        c->framerate = (AVRational){ CAM_FPS, 1 };
        c->pix_fmt = AV_PIX_FMT_YUV420P;
        c->bit_rate = bitrate;
        c->rc_max_rate = bitrate;
        c->rc_buffer_size = bitrate / 2;
        c->gop_size = CAM_FPS * 2;
        c->max_b_frames = 0;
        c->flags |= AV_CODEC_FLAG_LOW_DELAY;
        c->profile = AV_PROFILE_H264_CONSTRAINED_BASELINE;
        if (!strcmp(names[i], "libx264")) {
            av_opt_set(c->priv_data, "preset", "ultrafast", 0);
            av_opt_set(c->priv_data, "tune", "zerolatency", 0);
            av_opt_set(c->priv_data, "profile", "baseline", 0);
            av_opt_set(c->priv_data, "forced-idr", "1", 0);
            av_opt_set(c->priv_data, "x264-params", "repeat-headers=1", 0);
        } else if (!strcmp(names[i], "h264_nvenc")) {
            av_opt_set(c->priv_data, "preset", "p1", 0);
            av_opt_set(c->priv_data, "tune", "ll", 0);
            av_opt_set(c->priv_data, "zerolatency", "1", 0);
            av_opt_set(c->priv_data, "forced-idr", "1", 0);
        }
        if (avcodec_open2(c, codec, NULL) == 0) {
            return c;
        }
        avcodec_free_context(&c);
    }
    seterr(err, errlen, "no usable H.264 encoder in FFmpeg");
    return NULL;
}

void cam_close(struct cam *c) {
    if (!c) {
        return;
    }
    if (c->streaming) {
        enum v4l2_buf_type type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
        xioctl(c->fd, VIDIOC_STREAMOFF, &type);
    }
    for (int i = 0; i < c->nbuf; i++) {
        if (c->buf[i] && c->buf[i] != MAP_FAILED) {
            munmap(c->buf[i], c->len[i]);
        }
    }
    if (c->fd >= 0) {
        close(c->fd);
    }
    avcodec_free_context(&c->dec);
    av_packet_free(&c->dpkt);
    av_frame_free(&c->dframe);
    avcodec_free_context(&c->enc);
    av_packet_free(&c->epkt);
    av_frame_free(&c->eframe);
    sws_freeContext(c->sws);
    free(c->out);
    free(c);
}

struct cam *cam_open(const char *path, int bitrate, char *err, int errlen) {
    struct cam *c = calloc(1, sizeof(*c));
    if (!c) {
        seterr(err, errlen, "out of memory");
        return NULL;
    }
    pthread_once(&quiet_once, install_quiet_log);
    c->fd = open(path, O_RDWR | O_NONBLOCK);
    if (c->fd < 0) {
        seterr(err, errlen, "%s: %s", path, strerror(errno));
        free(c);
        return NULL;
    }

    c->fourcc = has_format(c->fd, V4L2_PIX_FMT_MJPEG) ? V4L2_PIX_FMT_MJPEG : V4L2_PIX_FMT_YUYV;
    struct v4l2_format fmt;
    memset(&fmt, 0, sizeof(fmt));
    fmt.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    fmt.fmt.pix.width = CAM_MAX_W;
    fmt.fmt.pix.height = CAM_MAX_H;
    fmt.fmt.pix.pixelformat = c->fourcc;
    fmt.fmt.pix.field = V4L2_FIELD_ANY;
    if (xioctl(c->fd, VIDIOC_S_FMT, &fmt) < 0 || fmt.fmt.pix.pixelformat != c->fourcc) {
        seterr(err, errlen, "%s: no MJPEG or YUYV capture", path);
        cam_close(c);
        return NULL;
    }
    c->w = fmt.fmt.pix.width;
    c->h = fmt.fmt.pix.height;

    struct v4l2_streamparm parm;
    memset(&parm, 0, sizeof(parm));
    parm.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    parm.parm.capture.timeperframe.numerator = 1;
    parm.parm.capture.timeperframe.denominator = CAM_FPS;
    xioctl(c->fd, VIDIOC_S_PARM, &parm);

    struct v4l2_requestbuffers req;
    memset(&req, 0, sizeof(req));
    req.count = CAM_BUFFERS;
    req.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    req.memory = V4L2_MEMORY_MMAP;
    if (xioctl(c->fd, VIDIOC_REQBUFS, &req) < 0 || req.count < 2) {
        seterr(err, errlen, "%s: VIDIOC_REQBUFS: %s", path, strerror(errno));
        cam_close(c);
        return NULL;
    }
    c->nbuf = req.count > CAM_BUFFERS ? CAM_BUFFERS : (int)req.count;
    for (int i = 0; i < c->nbuf; i++) {
        struct v4l2_buffer b;
        memset(&b, 0, sizeof(b));
        b.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
        b.memory = V4L2_MEMORY_MMAP;
        b.index = i;
        if (xioctl(c->fd, VIDIOC_QUERYBUF, &b) < 0) {
            seterr(err, errlen, "%s: VIDIOC_QUERYBUF: %s", path, strerror(errno));
            cam_close(c);
            return NULL;
        }
        c->len[i] = b.length;
        c->buf[i] = mmap(NULL, b.length, PROT_READ | PROT_WRITE, MAP_SHARED, c->fd, b.m.offset);
        if (c->buf[i] == MAP_FAILED || xioctl(c->fd, VIDIOC_QBUF, &b) < 0) {
            seterr(err, errlen, "%s: mmap/QBUF: %s", path, strerror(errno));
            cam_close(c);
            return NULL;
        }
    }
    enum v4l2_buf_type type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    if (xioctl(c->fd, VIDIOC_STREAMON, &type) < 0) {
        seterr(err, errlen, "%s: VIDIOC_STREAMON: %s", path, strerror(errno));
        cam_close(c);
        return NULL;
    }
    c->streaming = 1;

    if (c->fourcc == V4L2_PIX_FMT_MJPEG) {
        const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_MJPEG);
        c->dec = codec ? avcodec_alloc_context3(codec) : NULL;
        if (!c->dec || avcodec_open2(c->dec, codec, NULL) < 0) {
            seterr(err, errlen, "no MJPEG decoder in FFmpeg");
            cam_close(c);
            return NULL;
        }
        c->dpkt = av_packet_alloc();
        c->dframe = av_frame_alloc();
    }

    // At most 1280x720, aspect kept, even dimensions.
    int ew = c->w, eh = c->h;
    if (ew > CAM_MAX_W || eh > CAM_MAX_H) {
        if ((int64_t)ew * CAM_MAX_H > (int64_t)eh * CAM_MAX_W) {
            eh = (int)((int64_t)eh * CAM_MAX_W / ew);
            ew = CAM_MAX_W;
        } else {
            ew = (int)((int64_t)ew * CAM_MAX_H / eh);
            eh = CAM_MAX_H;
        }
    }
    ew &= ~1;
    eh &= ~1;
    c->enc = open_encoder(ew, eh, bitrate, err, errlen);
    if (!c->enc) {
        cam_close(c);
        return NULL;
    }
    c->epkt = av_packet_alloc();
    c->eframe = av_frame_alloc();
    c->eframe->format = c->enc->pix_fmt;
    c->eframe->width = ew;
    c->eframe->height = eh;
    if (av_frame_get_buffer(c->eframe, 0) < 0) {
        seterr(err, errlen, "out of memory");
        cam_close(c);
        return NULL;
    }
    return c;
}

const char *cam_encoder_name(struct cam *c) {
    return c->enc->codec->name;
}

void cam_size(struct cam *c, int *w, int *h, int *ew, int *eh) {
    *w = c->w;
    *h = c->h;
    *ew = c->enc->width;
    *eh = c->enc->height;
}

// Converts the dequeued picture into the encoder's frame; 0 on success.
static int convert(struct cam *c, const uint8_t *data, size_t len) {
    const uint8_t *src[4] = { 0 };
    int stride[4] = { 0 };
    int w = c->w, h = c->h;
    enum AVPixelFormat fmt;
    if (c->fourcc == V4L2_PIX_FMT_MJPEG) {
        c->dpkt->data = (uint8_t *)data;
        c->dpkt->size = (int)len;
        if (avcodec_send_packet(c->dec, c->dpkt) < 0 || avcodec_receive_frame(c->dec, c->dframe) < 0) {
            return -1;
        }
        for (int i = 0; i < 4; i++) {
            src[i] = c->dframe->data[i];
            stride[i] = c->dframe->linesize[i];
        }
        w = c->dframe->width;
        h = c->dframe->height;
        fmt = c->dframe->format;
    } else {
        if (len < (size_t)w * h * 2) {
            return -1;
        }
        src[0] = data;
        stride[0] = w * 2;
        fmt = AV_PIX_FMT_YUYV422;
    }
    c->sws = sws_getCachedContext(c->sws, w, h, fmt, c->enc->width, c->enc->height, c->enc->pix_fmt,
                                  SWS_FAST_BILINEAR, NULL, NULL, NULL);
    if (!c->sws || av_frame_make_writable(c->eframe) < 0) {
        return -1;
    }
    sws_scale(c->sws, src, stride, 0, h, c->eframe->data, c->eframe->linesize);
    if (c->fourcc == V4L2_PIX_FMT_MJPEG) {
        av_frame_unref(c->dframe);
    }
    return 0;
}

int cam_read_encode(struct cam *c, int force_key, uint8_t **out, int *key) {
    struct pollfd p = { .fd = c->fd, .events = POLLIN };
    int r = poll(&p, 1, 200);
    if (r < 0) {
        return errno == EINTR ? 0 : -1;
    }
    if (r == 0) {
        return 0;
    }
    if (p.revents & (POLLERR | POLLHUP)) {
        return -1;
    }

    struct v4l2_buffer b;
    memset(&b, 0, sizeof(b));
    b.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    b.memory = V4L2_MEMORY_MMAP;
    if (xioctl(c->fd, VIDIOC_DQBUF, &b) < 0) {
        return errno == EAGAIN ? 0 : -1;
    }
    int ok = b.index < (unsigned)c->nbuf && !(b.flags & V4L2_BUF_FLAG_ERROR) &&
             convert(c, c->buf[b.index], b.bytesused) == 0;
    if (xioctl(c->fd, VIDIOC_QBUF, &b) < 0) {
        return -1;
    }
    if (!ok) {
        // A corrupt or partial picture: skip it.
        return 0;
    }

    c->eframe->pts = c->pts++;
    c->eframe->pict_type = force_key ? AV_PICTURE_TYPE_I : AV_PICTURE_TYPE_NONE;
    if (avcodec_send_frame(c->enc, c->eframe) < 0) {
        return -1;
    }
    r = avcodec_receive_packet(c->enc, c->epkt);
    if (r == AVERROR(EAGAIN)) {
        return 0;
    }
    if (r < 0) {
        return -1;
    }
    if (c->epkt->size > c->outcap) {
        free(c->out);
        c->outcap = c->epkt->size * 2;
        c->out = malloc(c->outcap);
        if (!c->out) {
            c->outcap = 0;
            av_packet_unref(c->epkt);
            return -1;
        }
    }
    int n = c->epkt->size;
    memcpy(c->out, c->epkt->data, n);
    *key = (c->epkt->flags & AV_PKT_FLAG_KEY) != 0;
    av_packet_unref(c->epkt);
    *out = c->out;
    return n;
}
