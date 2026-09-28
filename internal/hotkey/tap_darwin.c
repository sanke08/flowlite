#include <ApplicationServices/ApplicationServices.h>
#include <stdbool.h>
#include <stdatomic.h>

extern void flowliteTapEvent(int keycode, bool down);
extern void flowliteCaptureEvent(int keycode, bool down);

// Device-specific modifier bits (IOKit NX_DEVICE*KEYMASK, IOLLEvent.h). These
// tell left from right, which the generic kCGEventFlagMask* bits do not. Fn
// has no left/right and is read from kCGEventFlagMaskSecondaryFn.
#define DEV_LCTRL  0x00000001
#define DEV_LSHIFT 0x00000002
#define DEV_RSHIFT 0x00000004
#define DEV_LCMD   0x00000008
#define DEV_RCMD   0x00000010
#define DEV_LALT   0x00000020
#define DEV_RALT   0x00000040
#define DEV_RCTRL  0x00002000

// modifierDown says whether a flags-changed event for keycode means that
// modifier is now down. Returns -1 for a keycode that is not a modifier we
// track (Caps Lock toggles rather than holds, so it is left out).
static int modifierDown(int keycode, CGEventFlags flags) {
    uint64_t mask;
    switch (keycode) {
        case 59: mask = DEV_LCTRL;  break;   // left control
        case 62: mask = DEV_RCTRL;  break;   // right control
        case 58: mask = DEV_LALT;   break;   // left option
        case 61: mask = DEV_RALT;   break;   // right option
        case 55: mask = DEV_LCMD;   break;   // left command
        case 54: mask = DEV_RCMD;   break;   // right command
        case 56: mask = DEV_LSHIFT; break;   // left shift
        case 60: mask = DEV_RSHIFT; break;   // right shift
        case 63: mask = kCGEventFlagMaskSecondaryFn; break; // fn / globe
        default: return -1;
    }
    return (flags & mask) != 0;
}

// ---- daemon tap ---------------------------------------------------------
//
// Lives on the main run loop. Only keycodes the chord cares about are passed
// to Go, so ordinary typing never wakes the daemon.

static CFMachPortRef      tap = NULL;
static CFRunLoopSourceRef src = NULL;
static bool               interest[128];

void flowlite_tap_clear_interest(void) {
    for (int i = 0; i < 128; i++) interest[i] = false;
}

void flowlite_tap_add_interest(int keycode) {
    if (keycode >= 0 && keycode < 128) interest[keycode] = true;
}

static CGEventRef callback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *ud) {
    (void)proxy; (void)ud;

    // macOS disables a tap it thinks is unresponsive; re-arm and carry on.
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (tap) CGEventTapEnable(tap, true);
        return event;
    }

    int keycode = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
    if (keycode < 0 || keycode >= 128 || !interest[keycode]) return event;

    if (type == kCGEventFlagsChanged) {
        int down = modifierDown(keycode, CGEventGetFlags(event));
        if (down >= 0) flowliteTapEvent(keycode, down);
        return event;
    }
    if (type == kCGEventKeyDown || type == kCGEventKeyUp) {
        flowliteTapEvent(keycode, type == kCGEventKeyDown);
    }
    return event;
}

bool flowlite_tap_start(void) {
    if (tap) return true;
    CGEventMask mask = CGEventMaskBit(kCGEventFlagsChanged)
                     | CGEventMaskBit(kCGEventKeyDown)
                     | CGEventMaskBit(kCGEventKeyUp);
    tap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap,
                           kCGEventTapOptionListenOnly, mask, callback, NULL);
    if (!tap) return false; // no Accessibility

    src = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, tap, 0);
    CFRunLoopAddSource(CFRunLoopGetMain(), src, kCFRunLoopCommonModes);
    CGEventTapEnable(tap, true);
    return true;
}

void flowlite_tap_stop(void) {
    if (!tap) return;
    CGEventTapEnable(tap, false);
    if (src) {
        CFRunLoopRemoveSource(CFRunLoopGetMain(), src, kCFRunLoopCommonModes);
        CFRelease(src);
        src = NULL;
    }
    CFRelease(tap);
    tap = NULL;
}

// ---- capture tap --------------------------------------------------------
//
// Used while the user is choosing a key, from `flowlite settings`, which has
// no Cocoa run loop on the main thread. The tap gets a run loop of its own on
// whichever thread calls flowlite_capture_open/run, and sees every key.

static CFMachPortRef      capTap = NULL;
static CFRunLoopSourceRef capSrc = NULL;
static atomic_bool        capStop;

static CGEventRef captureCallback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *ud) {
    (void)proxy; (void)ud;
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (capTap) CGEventTapEnable(capTap, true);
        return event;
    }
    int keycode = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);
    if (type == kCGEventFlagsChanged) {
        int down = modifierDown(keycode, CGEventGetFlags(event));
        if (down >= 0) flowliteCaptureEvent(keycode, down);
        return event;
    }
    if (type == kCGEventKeyDown || type == kCGEventKeyUp) {
        if (type == kCGEventKeyDown &&
            CGEventGetIntegerValueField(event, kCGKeyboardEventAutorepeat)) {
            return event;
        }
        flowliteCaptureEvent(keycode, type == kCGEventKeyDown);
    }
    return event;
}

// flowlite_capture_open creates the capture tap on the calling thread's run
// loop. False means macOS refused it (no Accessibility).
bool flowlite_capture_open(void) {
    if (capTap) return true;
    atomic_store(&capStop, false);
    CGEventMask mask = CGEventMaskBit(kCGEventFlagsChanged)
                     | CGEventMaskBit(kCGEventKeyDown)
                     | CGEventMaskBit(kCGEventKeyUp);
    capTap = CGEventTapCreate(kCGSessionEventTap, kCGHeadInsertEventTap,
                              kCGEventTapOptionListenOnly, mask, captureCallback, NULL);
    if (!capTap) return false;
    capSrc = CFMachPortCreateRunLoopSource(kCFAllocatorDefault, capTap, 0);
    CFRunLoopAddSource(CFRunLoopGetCurrent(), capSrc, kCFRunLoopDefaultMode);
    CGEventTapEnable(capTap, true);
    return true;
}

// flowlite_capture_run pumps the calling thread's run loop until
// flowlite_capture_stop is called, then tears the tap down. Short slices
// rather than one CFRunLoopRun, so a stop that lands before the loop starts
// is not lost.
void flowlite_capture_run(void) {
    while (!atomic_load(&capStop)) {
        CFRunLoopRunInMode(kCFRunLoopDefaultMode, 0.05, false);
    }
    if (capTap) {
        CGEventTapEnable(capTap, false);
        if (capSrc) {
            CFRunLoopRemoveSource(CFRunLoopGetCurrent(), capSrc, kCFRunLoopDefaultMode);
            CFRelease(capSrc);
            capSrc = NULL;
        }
        CFRelease(capTap);
        capTap = NULL;
    }
}

void flowlite_capture_stop(void) {
    atomic_store(&capStop, true);
}
