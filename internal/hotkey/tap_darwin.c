#include <ApplicationServices/ApplicationServices.h>
#include <stdbool.h>

extern void flowliteTapEvent(int keycode, bool down);

// Device-specific modifier bits (IOKit NX_DEVICE*KEYMASK). These tell left
// from right, which the generic kCGEventFlagMask* bits do not.
#define DEV_RSHIFT 0x00000004
#define DEV_RCMD   0x00000010
#define DEV_RALT   0x00000040
#define DEV_RCTRL  0x00002000

static CFMachPortRef      tap = NULL;
static CFRunLoopSourceRef src = NULL;

static CGEventRef callback(CGEventTapProxy proxy, CGEventType type, CGEventRef event, void *ud) {
    (void)proxy; (void)ud;

    // macOS disables a tap it thinks is unresponsive; re-arm and carry on.
    if (type == kCGEventTapDisabledByTimeout || type == kCGEventTapDisabledByUserInput) {
        if (tap) CGEventTapEnable(tap, true);
        return event;
    }

    int keycode = (int)CGEventGetIntegerValueField(event, kCGKeyboardEventKeycode);

    if (type == kCGEventFlagsChanged) {
        CGEventFlags flags = CGEventGetFlags(event);
        uint64_t mask = 0;
        switch (keycode) {
            case 61: mask = DEV_RALT;   break;   // right option
            case 62: mask = DEV_RCTRL;  break;   // right control
            case 54: mask = DEV_RCMD;   break;   // right command
            case 60: mask = DEV_RSHIFT; break;   // right shift
            default: return event;
        }
        flowliteTapEvent(keycode, (flags & mask) != 0);
        return event;
    }

    if (type == kCGEventKeyDown || type == kCGEventKeyUp) {
        // Only the keys the daemon can care about: F13–F15 and Escape.
        if (keycode == 105 || keycode == 107 || keycode == 113 || keycode == 53) {
            flowliteTapEvent(keycode, type == kCGEventKeyDown);
        }
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
