package service

// upscaleModeMetalSet is wired up by metal_video_darwin.go's init() to
// MetalVideoSetUpscaleMode -- same "platform-agnostic core, thin platform
// push hook" split as ai_vision.go's aiVisionMetalPush. nil on every
// platform without a metal_video_darwin.go (Linux, Windows, Android, iOS),
// where there is no upscale-quality picker in the UI at all yet (see
// video_start_dialog.go's upscaleSelect construction, macOS-only for now)
// and SetUpscaleMode below is simply a no-op.
var upscaleModeMetalSet func(mode string)

// SetUpscaleMode selects how the decoded video frame is resized to fit the
// window/display when it isn't already an exact pixel match (see
// models.UpscaleMode* for the possible values). Called from
// startVideoWithParamsInternal alongside SetColor444/SetHdr, but unlike
// those two, this has no codec-negotiation dependency -- it takes effect as
// soon as it's called, without needing to wait for a (re)connection.
func SetUpscaleMode(mode string) {
	if set := upscaleModeMetalSet; set != nil {
		set(mode)
	}
}
