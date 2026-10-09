#ifndef USBRIDGE_UPLINK_CAMERA_LINUX_H
#define USBRIDGE_UPLINK_CAMERA_LINUX_H

#include <stdint.h>

struct cam;

// 1 if path is a V4L2 node that captures video (not a metadata node).
int cam_is_capture_device(const char *path);
// Opens the camera and an H.264 encoder at bitrate bits/s; NULL with err set.
struct cam *cam_open(const char *path, int bitrate, char *err, int errlen);
const char *cam_encoder_name(struct cam *c);
// The camera's picture size and the encoded size.
void cam_size(struct cam *c, int *w, int *h, int *ew, int *eh);
// Waits up to 200ms for a picture and encodes it. Returns the access unit's
// size (*out points into the camera's own buffer, valid until the next
// call; *key set on a keyframe), 0 when there is nothing yet, -1 when the
// camera is gone.
int cam_read_encode(struct cam *c, int force_key, uint8_t **out, int *key);
void cam_close(struct cam *c);

#endif
