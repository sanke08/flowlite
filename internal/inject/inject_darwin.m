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

// ---- typing -------------------------------------------------------------
//
// Live transcription types its words instead of pasting them, so they land as
// they are spoken and the clipboard is never touched. Each event carries up
// to kChunk UTF-16 units of text (CGEventKeyboardSetUnicodeString's own
// limit is 20) on a key code no hotkey uses; the receiving app inserts the
// string whatever the key code. The events come from a private source with
// their modifier flags cleared, because the user is usually still holding
// the dictation key — often Option — and Option-held text means something
// else to many apps.

static bool postKey(CGEventSourceRef src, CGKeyCode key, const UniChar *chars, UniCharCount n) {
    CGEventRef down = CGEventCreateKeyboardEvent(src, key, true);
    CGEventRef up   = CGEventCreateKeyboardEvent(src, key, false);
    bool ok = down && up;
    if (ok) {
        CGEventSetFlags(down, 0);
        CGEventSetFlags(up, 0);
        if (chars) {
            CGEventKeyboardSetUnicodeString(down, n, chars);
            CGEventKeyboardSetUnicodeString(up, n, chars);
        }
        CGEventPost(kCGSessionEventTap, down);
        CGEventPost(kCGSessionEventTap, up);
    }
    if (down) CFRelease(down);
    if (up)   CFRelease(up);
    return ok;
}

bool flowlite_type_text(const char *utf8) {
    if (!AXIsProcessTrusted()) return false;
    @autoreleasepool {
        NSString *s = [NSString stringWithUTF8String:utf8];
        if (!s) return false;
        enum { kChunk = 16 };
        CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStatePrivate);
        NSUInteger len = [s length];
        bool ok = true;
        for (NSUInteger at = 0; at < len && ok; ) {
            NSUInteger n = MIN((NSUInteger)kChunk, len - at);
            // Never split a surrogate pair across two events.
            if (at + n < len && CFStringIsSurrogateHighCharacter([s characterAtIndex:at + n - 1])) n--;
            UniChar buf[kChunk];
            [s getCharacters:buf range:NSMakeRange(at, n)];
            ok = postKey(src, 0, buf, (UniCharCount)n);
            at += n;
            usleep(2000); // a breath between events, so slow apps keep up
        }
        if (src) CFRelease(src);
        return ok;
    }
}

bool flowlite_backspace(int n) {
    if (!AXIsProcessTrusted()) return false;
    const CGKeyCode kDelete = 51;
    CGEventSourceRef src = CGEventSourceCreate(kCGEventSourceStatePrivate);
    bool ok = true;
    for (int i = 0; i < n && ok; i++) {
        ok = postKey(src, kDelete, NULL, 0);
        usleep(1500);
    }
    if (src) CFRelease(src);
    return ok;
}
