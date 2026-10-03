//go:build darwin && !ios

package platform

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework IOKit -framework CoreFoundation

#include <IOKit/hid/IOHIDManager.h>
#include <IOKit/hid/IOHIDQueue.h>
#include <IOKit/IOKitLib.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <string.h>

typedef struct {
    IOHIDDeviceRef device;
    IOHIDQueueRef  queue;
    // Raw HID state, NOT yet given Xbox/DualShock/etc. meaning -- that
    // translation happens in Go via sdlMapping.capture() (gamepad_sdlmap.go),
    // the same per-VID/PID SDL_GameControllerDB-driven logic
    // gamepad_capture_windows.go already uses. Button bit n (0-indexed) is
    // HID button usage n+1, matching sdlSource{kind:sdlButton}'s "bn"
    // directly. axis[0..5] is Generic Desktop X,Y,Z,Rx,Ry,Rz in HID usage
    // order (NOT WinMM's report-declaration-order quirk -- see
    // sdlAxisToJoy's own doc comment -- IOKit hands us usage-tagged values
    // directly), each scaled to -1..1, matching SDL's own a0..a5 axis
    // numbering convention. pov is the hat in hundredths of a degree, or -1
    // when centred, exactly what joyInput.pov/hatMask expect.
    uint32_t buttons;
    double   axis[6];
    int      pov;
    uint16_t vendorID;
    uint16_t productID;
} HIDCapture;

static double scaleUnit(CFIndex val, CFIndex lo, CFIndex hi) {
    if (hi <= lo) return 0;
    CFIndex mid = (hi + lo) / 2;
    CFIndex half = (hi - lo) / 2;
    if (half == 0) return 0;
    double r = (double)(val - mid) / (double)half;
    if (r >  1) r =  1;
    if (r < -1) r = -1;
    return r;
}

static void processValue(HIDCapture* cap, IOHIDValueRef value) {
    IOHIDElementRef elem  = IOHIDValueGetElement(value);
    uint32_t page  = IOHIDElementGetUsagePage(elem);
    uint32_t usage = IOHIDElementGetUsage(elem);
    CFIndex  ival  = IOHIDValueGetIntegerValue(value);
    CFIndex  lo    = IOHIDElementGetLogicalMin(elem);
    CFIndex  hi    = IOHIDElementGetLogicalMax(elem);

    if (page == kHIDPage_Button) {
        uint32_t idx = usage - 1; // 0-indexed, matches sdlSource's "bN"
        if (idx < 32) {
            if (ival) cap->buttons |= (1u << idx);
            else      cap->buttons &= ~(1u << idx);
        }
        return;
    }

    if (page == kHIDPage_GenericDesktop) {
        switch (usage) {
            case kHIDUsage_GD_X:  cap->axis[0] = scaleUnit(ival, lo, hi); break;
            case kHIDUsage_GD_Y:  cap->axis[1] = scaleUnit(ival, lo, hi); break;
            case kHIDUsage_GD_Z:  cap->axis[2] = scaleUnit(ival, lo, hi); break;
            case kHIDUsage_GD_Rx: cap->axis[3] = scaleUnit(ival, lo, hi); break;
            case kHIDUsage_GD_Ry: cap->axis[4] = scaleUnit(ival, lo, hi); break;
            case kHIDUsage_GD_Rz: cap->axis[5] = scaleUnit(ival, lo, hi); break;
            case kHIDUsage_GD_Hatswitch:
                // HID hat switches report 0-7 for the eight directions and
                // some out-of-range "null" value (commonly 8) when centred.
                cap->pov = (ival >= 0 && ival <= 7) ? (int)(ival * 4500) : -1;
                break;
        }
    }
}

// openCapture finds the gamepad by its IOKit registry entry ID, retains it, and
// opens an HID queue. Using a registry entry ID (instead of a raw IOHIDDeviceRef
// pointer) avoids dangling-pointer crashes when the enumerating IOHIDManager has
// been closed and released before capture starts.
static HIDCapture* openCapture(uint64_t registryEntryID) {
    // Create a temporary manager to locate the device by its stable registry ID.
    IOHIDManagerRef mgr = IOHIDManagerCreate(kCFAllocatorDefault, kIOHIDOptionsTypeNone);
    if (!mgr) return NULL;

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
        CFDictionarySetValue(m, CFSTR(kIOHIDDeviceUsageKey), uRef);
        CFRelease(upRef); CFRelease(uRef);
        CFArrayAppendValue(matchers, m);
        CFRelease(m);
    }
    IOHIDManagerSetDeviceMatchingMultiple(mgr, matchers);
    CFRelease(matchers);
    IOHIDManagerOpen(mgr, kIOHIDOptionsTypeNone);

    CFSetRef devices = IOHIDManagerCopyDevices(mgr);
    IOHIDDeviceRef dev = NULL;
    if (devices) {
        CFIndex n = CFSetGetCount(devices);
        IOHIDDeviceRef* devs = (IOHIDDeviceRef*)malloc(n * sizeof(IOHIDDeviceRef));
        CFSetGetValues(devices, (const void**)devs);
        for (CFIndex i = 0; i < n; i++) {
            io_service_t svc = IOHIDDeviceGetService(devs[i]);
            uint64_t entryID = 0;
            if (svc != IO_OBJECT_NULL) {
                IORegistryEntryGetRegistryEntryID(svc, &entryID);
            }
            if (entryID == registryEntryID) {
                dev = devs[i];
                CFRetain(dev); // keep alive after manager is released
                break;
            }
        }
        free(devs);
        CFRelease(devices);
    }
    IOHIDManagerClose(mgr, kIOHIDOptionsTypeNone);
    CFRelease(mgr); // manager gone, dev survives via our CFRetain

    if (!dev) return NULL;

    int vid = 0, pid = 0;
    CFNumberRef vidRef = (CFNumberRef)IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDVendorIDKey));
    if (vidRef) CFNumberGetValue(vidRef, kCFNumberIntType, &vid);
    CFNumberRef pidRef = (CFNumberRef)IOHIDDeviceGetProperty(dev, CFSTR(kIOHIDProductIDKey));
    if (pidRef) CFNumberGetValue(pidRef, kCFNumberIntType, &pid);

    IOReturn ret = IOHIDDeviceOpen(dev, kIOHIDOptionsTypeNone);
    if (ret != kIOReturnSuccess) {
        CFRelease(dev);
        return NULL;
    }

    IOHIDQueueRef queue = IOHIDQueueCreate(kCFAllocatorDefault, dev, 64, kIOHIDOptionsTypeNone);
    if (!queue) {
        IOHIDDeviceClose(dev, kIOHIDOptionsTypeNone);
        CFRelease(dev);
        return NULL;
    }

    CFArrayRef elems = IOHIDDeviceCopyMatchingElements(dev, NULL, kIOHIDOptionsTypeNone);
    if (elems) {
        CFIndex n = CFArrayGetCount(elems);
        for (CFIndex i = 0; i < n; i++) {
            IOHIDElementRef elem = (IOHIDElementRef)CFArrayGetValueAtIndex(elems, i);
            IOHIDQueueAddElement(queue, elem);
        }
        CFRelease(elems);
    }

    IOHIDQueueStart(queue);

    HIDCapture* cap = (HIDCapture*)calloc(1, sizeof(HIDCapture));
    cap->device = dev;
    cap->queue  = queue;
    cap->pov    = -1;
    cap->vendorID  = (uint16_t)vid;
    cap->productID = (uint16_t)pid;
    return cap;
}

// pollCapture drains the HID queue and writes the latest accumulated state into cap.
static void pollCapture(HIDCapture* cap) {
    if (!cap || !cap->queue) return;
    IOHIDValueRef value;
    while ((value = IOHIDQueueCopyNextValueWithTimeout(cap->queue, 0)) != NULL) {
        processValue(cap, value);
        CFRelease(value);
    }
}

static void getIdentity(HIDCapture* cap, uint16_t* vid, uint16_t* pid) {
    if (!cap) { *vid = 0; *pid = 0; return; }
    *vid = cap->vendorID;
    *pid = cap->productID;
}

static void getRawState(HIDCapture* cap, uint32_t* buttons, double* axis, int* pov) {
    if (!cap) {
        *buttons = 0;
        for (int i = 0; i < 6; i++) axis[i] = 0;
        *pov = -1;
        return;
    }
    *buttons = cap->buttons;
    for (int i = 0; i < 6; i++) axis[i] = cap->axis[i];
    *pov = cap->pov;
}

// closeCapture releases resources.
static void closeCapture(HIDCapture* cap) {
    if (!cap) return;
    if (cap->queue) {
        IOHIDQueueStop(cap->queue);
        CFRelease(cap->queue);
    }
    if (cap->device) {
        IOHIDDeviceClose(cap->device, kIOHIDOptionsTypeNone);
        CFRelease(cap->device); // balance the CFRetain in openCapture
    }
    free(cap);
}
*/
import "C"
import (
	"fmt"
	"time"

	"github.com/sirupsen/logrus"
)

// GamepadCaptureState holds the current decoded state of a gamepad.
type GamepadCaptureState struct {
	Buttons                   uint16
	LeftX, LeftY              int16
	RightX, RightY            int16
	LeftTrigger, RightTrigger uint8
}

// GamepadCapture manages an active IOKit HID capture for one gamepad.
type GamepadCapture struct {
	cctx *C.HIDCapture
	stop chan struct{}
	done chan struct{}
}

// genericXboxMapping is the fallback layout for a HID gamepad the SDL
// database (gamepad_sdldb.go) does not recognize: Generic Desktop axes in
// HID usage order (X,Y,Z,Rx,Ry,Rz) and buttons 1-11 in Xbox's own usage
// order -- the same assumption gamepad_capture_windows.go's pollWinMMState
// falls back to for an unknown DirectInput pad. Built from a literal SDL
// mapping line so an unrecognized pad goes through the exact same
// sdlMapping.capture() path a database hit does (including the
// lefty/righty Moonlight-vs-DirectInput Y flip -- see that method's own
// doc comment), instead of a second hand-rolled implementation.
var genericXboxMapping = func() *sdlMapping {
	_, m, ok := parseSDLMapping("00000000000000000000000000000000,Generic Xbox Layout," +
		"a:b0,b:b1,x:b2,y:b3,leftshoulder:b4,rightshoulder:b5,back:b6,start:b7," +
		"leftstick:b8,rightstick:b9,guide:b10," +
		"leftx:a0,lefty:a1,rightx:a3,righty:a4,lefttrigger:a2,righttrigger:a5,")
	if !ok {
		panic("gamepad_capture_darwin: invalid built-in generic Xbox SDL mapping")
	}
	return m
}()

// StartGamepadCapture opens the gamepad identified by deviceID and calls onState
// at ~60 Hz with the latest input state. Returns an error if the device cannot be opened.
func StartGamepadCapture(deviceID string, onState func(GamepadCaptureState)) (*GamepadCapture, error) {
	var id uint64
	if _, err := fmt.Sscanf(deviceID, "%d", &id); err != nil || id == 0 {
		return nil, fmt.Errorf("invalid gamepad device ID %q", deviceID)
	}

	cctx := C.openCapture(C.uint64_t(id))
	if cctx == nil {
		return nil, fmt.Errorf("failed to open IOKit HID device %s", deviceID)
	}

	var cvid, cpid C.uint16_t
	C.getIdentity(cctx, &cvid, &cpid)
	vid, pid := uint16(cvid), uint16(cpid)

	// A pad the SDL database knows is read through its mapping (a PlayStation-
	// layout pad has its triggers, right stick and face buttons elsewhere than
	// an Xbox pad); an unknown one keeps the generic Xbox-style layout --
	// mirrors gamepad_capture_windows.go's own WinMM capture exactly.
	mapping := sdlMappingFor(vid, pid)
	if mapping != nil {
		logrus.Infof("🎮 [IOKit] gamepad %04x:%04x uses the SDL mapping %q", vid, pid, mapping.name)
	} else {
		mapping = genericXboxMapping
		logrus.Infof("🎮 [IOKit] gamepad %04x:%04x has no SDL mapping, assuming the Xbox layout", vid, pid)
	}

	cap := &GamepadCapture{
		cctx: cctx,
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}

	go func() {
		defer func() {
			C.closeCapture(cap.cctx)
			close(cap.done)
		}()

		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-cap.stop:
				return
			case <-ticker.C:
				C.pollCapture(cctx)

				var buttons C.uint32_t
				var axis [6]C.double
				var pov C.int
				C.getRawState(cctx, &buttons, &axis[0], &pov)

				raw := joyInput{
					axes: [6]float64{
						float64(axis[0]), float64(axis[1]), float64(axis[2]),
						float64(axis[3]), float64(axis[4]), float64(axis[5]),
					},
					buttons: uint32(buttons),
					pov:     int(pov),
				}
				onState(mapping.capture(raw))
			}
		}
	}()

	return cap, nil
}

// Stop halts the capture goroutine and closes the HID device.
func (c *GamepadCapture) Stop() {
	close(c.stop)
	<-c.done
}
