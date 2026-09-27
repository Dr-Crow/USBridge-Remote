//go:build js && wasm

package api

// usbClientDataChannelFallback is false on the wasm build: a browser
// session's WebRTC DataChannel is the ONLY transport USBClient uses once
// SetOpenDataChannel is wired (see gui.attachUSBClient) -- there is no
// direct/Tailscale HTTP fallback for the web client, that's desktop-native
// only. Before this, a request made while no PeerConnection/DataChannel
// existed yet (e.g. the dashboard polling drives/local, device/info,
// iso/space, device/status, pcpanel/leds right after connecting, before the
// user has pressed Start on the video widget) fell back to a direct
// fetch() -- which a relay/remote session (https-loaded page, private-
// network agent) always has blocked by the browser (mixed content, or
// Chrome's Local Network Access policy), showing up as an endless stream of
// "(blocked)" entries in devtools without ever succeeding. Failing the
// request outright instead (see webrtcAPITransport.RoundTrip) is the
// correct behavior here: the dashboard simply has nothing to show until
// video/WebRTC connects, rather than silently retrying a request that can
// never work.
const usbClientDataChannelFallback = false
