//go:build !(js && wasm)

package api

// usbClientDataChannelFallback: desktop-native builds keep the pre-existing
// behavior of falling back to the normal Direct/Tailscale network transport
// whenever the WebRTC DataChannel opener errors -- which is always, since
// desktop-native's video client is MoonlightService, whose OpenDataChannel
// unconditionally errors (see its own doc comment: "DataChannel not
// supported on MoonlightService"). So every request already went through
// this fallback before webrtcAPITransport ever existed; nothing changes
// here. See usb_client_fallback_wasm.go for the browser build counterpart.
const usbClientDataChannelFallback = true
