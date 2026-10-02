package streamhost

// ListCaptureDevices correlates punktfunk-host's view of capturable outputs
// against the agent's own independent display enumeration. Punktfunk does
// have a real command for this on Linux (`punktfunk-host list-monitors`,
// confirmed in main.rs's usage text), but its output format wasn't parsed
// this pass -- see agent/docs/PUNKTFUNK_BACKEND_TODO.md. An empty list is
// the same "not supported yet" answer CaptureDeviceLister's doc comment
// already allows for a backend with nothing to report; monitor selection
// still works through Punktfunk's own auto-detection.
func (b *punktfunkBackend) ListCaptureDevices() []CaptureDevice { return nil }
