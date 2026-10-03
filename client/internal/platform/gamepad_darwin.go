//go:build darwin && !ios

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>

// enumerateGamepads fills buf with up to maxDevices device IDs (uint64),
// copies the corresponding name into names[i] (caller must free), and fills
// vendorIDs/productIDs (0 when IOKit has no value for that device). Returns
// count.
static int enumerateGamepads(uint64_t* ids, char** names, int* vendorIDs, int* productIDs, int maxDevices) {
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return 0;

    // Match gamepads and joysticks (Usage Page 1, Usage 4 or 5)
    CFMutableArrayRef matchers = CFArrayCreateMutable(kCFAllocatorDefault, 2, &kCFTypeArrayCallBacks);

    int usages[] = {kHIDUsage_GD_GamePad, kHIDUsage_GD_Joystick};
    for (int i = 0; i < 2; i++) {
        CFMutableDictionaryRef m = CFDictionaryCreateMutable(kCFAllocatorDefault, 2,
                                    &kCFTypeDictionaryKeyCallBacks,
                                    &kCFTypeDictionaryValueCallBacks);
        int up = kHIDPage_GenericDesktop;
        CFNumberRef upRef = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &up);
        CFNumberRef uRef  = CFNumberCreate(kCFAllocatorDefault, kCFNumberIntType, &usages[i]);
        CFDictionarySetValue(m, CFSTR(kIOHIDDeviceUsagePageKey), upRef);
        CFDictionarySetValue(m, CFSTR(kIOHIDDeviceUsageKey),     uRef);
        CFRelease(upRef); CFRelease(uRef);
        CFArrayAppendValue(matchers, m);
        CFRelease(m);
    }
    IOHIDManagerSetDeviceMatchingMultiple(mgr, matchers);
    CFRelease(matchers);

    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devices = IOHIDManagerCopyDevices(mgr);
    int count = 0;
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
        CFSetGetValues(devices, (const void**)devs);
        for (CFIndex i = 0; i < n && count < maxDevices; i++) {
            io_service_t svc = IOHIDDeviceGetService(devs[i]);
            uint64_t entryID = 0;
            if (svc != IO_OBJECT_NULL) {
                IORegistryEntryGetRegistryEntryID(svc, &entryID);
            }
            if (entryID == 0) continue;
            ids[count] = entryID;
            CFStringRef nameRef = (CFStringRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDProductKey));
            if (nameRef) {
                char buf[256] = {0};
                CFStringGetCString(nameRef, buf, sizeof(buf), kCFStringEncodingUTF8);
                names[count] = strdup(buf);
            } else {
                names[count] = strdup("Unknown Gamepad");
            }
            int vid = 0, pid = 0;
            CFNumberRef vidRef = (CFNumberRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDVendorIDKey));
            if (vidRef) CFNumberGetValue(vidRef, kCFNumberIntType, &vid);
            CFNumberRef pidRef = (CFNumberRef)IOHIDDeviceGetProperty(devs[i], CFSTR(kIOHIDProductIDKey));
            if (pidRef) CFNumberGetValue(pidRef, kCFNumberIntType, &pid);
            vendorIDs[count] = vid;
            productIDs[count] = pid;
            count++;
        }
        free(devs);
        CFRelease(devices);
    }
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr);
    return count;
}
*/
import "C"
import (
	"fmt"
	"sort"
	"unsafe"
)

// GamepadDevice describes a system gamepad.
type GamepadDevice struct {
	ID        string
	Name      string
	VendorID  string // e.g. "0x045e" (empty if not available via IOKit)
	ProductID string // e.g. "0x028e"
}

// EnumerateGamepads returns all gamepads currently connected to the system.
func EnumerateGamepads() []GamepadDevice {
	const maxDevices = 16
	ids := make([]C.uint64_t, maxDevices)
	names := make([]*C.char, maxDevices)
	vendorIDs := make([]C.int, maxDevices)
	productIDs := make([]C.int, maxDevices)

	count := int(C.enumerateGamepads(&ids[0], &names[0], &vendorIDs[0], &productIDs[0], C.int(maxDevices)))

	type rawDevice struct {
		id        uint64
		name      string
		vendorID  int
		productID int
	}
	raw := make([]rawDevice, count)
	for i := 0; i < count; i++ {
		name := C.GoString(names[i])
		C.free(unsafe.Pointer(names[i]))
		raw[i] = rawDevice{id: uint64(ids[i]), name: name, vendorID: int(vendorIDs[i]), productID: int(productIDs[i])}
	}
	// IOHIDManagerCopyDevices hands back a CFSet, which has no defined
	// iteration order -- it can (and does) vary between calls even with the
	// exact same devices attached. Left unsorted, that reshuffles the row
	// order in the devices list on every ~1s poll, so a toggle tap can land
	// on a different row than the one the user tapped a moment later. IDs
	// are each device's stable IORegistryEntryID, so sorting by ID keeps the
	// list order constant across polls as long as the device set itself
	// hasn't changed.
	sort.Slice(raw, func(i, j int) bool { return raw[i].id < raw[j].id })

	result := make([]GamepadDevice, 0, count)
	for _, d := range raw {
		dev := GamepadDevice{
			ID:   fmt.Sprintf("%d", d.id),
			Name: d.name,
		}
		// 0 means IOKit had no kIOHIDVendorIDKey/kIOHIDProductIDKey for this
		// device (some virtual/synthetic pads, e.g. a vendor driver's XInput
		// compatibility shim, don't expose one) -- leave it blank rather than
		// reporting a bogus "0x0000", which gamepadIdentityMatches (disk_widget_gamepad.go)
		// would otherwise treat as a real id and wrongly match/mismatch against it.
		if d.vendorID != 0 {
			dev.VendorID = fmt.Sprintf("0x%04x", d.vendorID)
		}
		if d.productID != 0 {
			dev.ProductID = fmt.Sprintf("0x%04x", d.productID)
		}
		result = append(result, dev)
	}
	return result
}
