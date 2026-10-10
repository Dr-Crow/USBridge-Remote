//go:build !js

package controller

// usbSetupSupported: "Over USB" in the Add Connection dialog (desktop; the
// dialog hides it on phones, where the KVM's cable doesn't go).
const usbSetupSupported = true
