#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <stdlib.h>
#include <string.h>

char *flowlite_clipboard_get(void) {
    @autoreleasepool {
        NSString *s = [[NSPasteboard generalPasteboard] stringForType:NSPasteboardTypeString];
        if (s == nil) return NULL;
        const char *utf8 = [s UTF8String];
        return utf8 ? strdup(utf8) : NULL;
    }
}

void flowlite_clipboard_set(const char *text) {
    @autoreleasepool {
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        [pb setString:[NSString stringWithUTF8String:text] forType:NSPasteboardTypeString];
    }
}

// Cmd+V, posted at the HID level so the frontmost app receives it exactly as
// if the user had typed it.
void flowlite_paste_keystroke(void) {
    const CGKeyCode kV = 9;
    const CGKeyCode kCmd = 55;
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);

    CGEventRef cmdDown = CGEventCreateKeyboardEvent(src, kCmd, true);
    CGEventRef vDown   = CGEventCreateKeyboardEvent(src, kV, true);
    CGEventRef vUp     = CGEventCreateKeyboardEvent(src, kV, false);
    CGEventRef cmdUp   = CGEventCreateKeyboardEvent(src, kCmd, false);
    CGEventSetFlags(vDown, kCGEventFlagMaskCommand);
    CGEventSetFlags(vUp, kCGEventFlagMaskCommand);

    CGEventPost(kCGHIDEventTap, cmdDown);
    CGEventPost(kCGHIDEventTap, vDown);
    CGEventPost(kCGHIDEventTap, vUp);
    CGEventPost(kCGHIDEventTap, cmdUp);

    CFRelease(cmdDown); CFRelease(vDown); CFRelease(vUp); CFRelease(cmdUp);
    if (src) CFRelease(src);
}
