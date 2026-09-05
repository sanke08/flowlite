#import <AppKit/AppKit.h>
#import <ApplicationServices/ApplicationServices.h>
#include <stdbool.h>
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

// Returns false when the write did not take. AppKit can refuse — another
// process owning the pasteboard, a sandbox denial — and reporting success
// there means pasting the *previous* clipboard into the user's document while
// the pill says everything worked.
bool flowlite_clipboard_set(const char *text) {
    @autoreleasepool {
        if (text == NULL) return false;
        NSString *s = [NSString stringWithUTF8String:text];
        if (s == nil) return false;
        NSPasteboard *pb = [NSPasteboard generalPasteboard];
        [pb clearContents];
        return [pb setString:s forType:NSPasteboardTypeString] ? true : false;
    }
}

// The pasteboard's change counter. It moves every time anyone writes, so
// comparing it before and after tells us whether what we put there is still
// what is there — the only safe basis for restoring the old contents.
long flowlite_clipboard_serial(void) {
    @autoreleasepool {
        return (long)[[NSPasteboard generalPasteboard] changeCount];
    }
}

// Cmd+V, posted at the HID level so the frontmost app receives it exactly as
// if the user had typed it.
//
// CGEventPost cannot report failure: without Accessibility it silently does
// nothing. So the permission is checked first — otherwise a user who revokes
// it mid-session gets the success chime and no text, forever.
bool flowlite_paste_keystroke(void) {
    if (!AXIsProcessTrusted()) return false;

    const CGKeyCode kV = 9;
    const CGKeyCode kCmd = 55;
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStateHIDSystemState);

    CGEventRef cmdDown = CGEventCreateKeyboardEvent(src, kCmd, true);
    CGEventRef vDown   = CGEventCreateKeyboardEvent(src, kV, true);
    CGEventRef vUp     = CGEventCreateKeyboardEvent(src, kV, false);
    CGEventRef cmdUp   = CGEventCreateKeyboardEvent(src, kCmd, false);
    bool ok = cmdDown && vDown && vUp && cmdUp;

    if (ok) {
        CGEventSetFlags(vDown, kCGEventFlagMaskCommand);
        CGEventSetFlags(vUp, kCGEventFlagMaskCommand);
        CGEventPost(kCGHIDEventTap, cmdDown);
        CGEventPost(kCGHIDEventTap, vDown);
        CGEventPost(kCGHIDEventTap, vUp);
        CGEventPost(kCGHIDEventTap, cmdUp);
    }

    if (cmdDown) CFRelease(cmdDown);
    if (vDown)   CFRelease(vDown);
    if (vUp)     CFRelease(vUp);
    if (cmdUp)   CFRelease(cmdUp);
    if (src)     CFRelease(src);
    return ok;
}
