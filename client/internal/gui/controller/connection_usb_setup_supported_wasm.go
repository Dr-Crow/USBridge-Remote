//go:build js

package controller

// usbSetupSupported is false in the browser: a page can't reach the KVM's
// http://10.55.0.1 (mixed content from an https page, no CORS on the setup
// server). Open http://10.55.0.1 in the browser itself instead.
const usbSetupSupported = false
