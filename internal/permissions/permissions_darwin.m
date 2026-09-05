#import <ApplicationServices/ApplicationServices.h>
#import <AVFoundation/AVFoundation.h>
#include <stdbool.h>

bool flowlite_ax_trusted(void) {
    return AXIsProcessTrusted();
}

bool flowlite_ax_request(void) {
    @autoreleasepool {
        NSDictionary *opts = @{ (__bridge NSString *)kAXTrustedCheckOptionPrompt : @YES };
        return AXIsProcessTrustedWithOptions((__bridge CFDictionaryRef)opts);
    }
}

// ---- microphone ------------------------------------------------------------
// Recording without this permission does not fail: the device opens and
// delivers silence forever, which looks exactly like a broken app. So it has
// to be asked about explicitly rather than discovered.

int flowlite_mic_status(void) {
    switch ([AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeAudio]) {
        case AVAuthorizationStatusAuthorized:    return 1;
        case AVAuthorizationStatusDenied:        return 2;
        case AVAuthorizationStatusRestricted:    return 3;
        case AVAuthorizationStatusNotDetermined:
        default:                                 return 0;
    }
}

// Shows the system prompt and waits for the answer, so setup can tell the user
// what happened instead of carrying on into a silent microphone.
bool flowlite_mic_request(void) {
    @autoreleasepool {
        __block bool granted = false;
        dispatch_semaphore_t done = dispatch_semaphore_create(0);
        [AVCaptureDevice requestAccessForMediaType:AVMediaTypeAudio
                                 completionHandler:^(BOOL ok) {
            granted = ok;
            dispatch_semaphore_signal(done);
        }];
        // Never wait forever. `flowlite start` spawns a detached child with no
        // terminal; if the system cannot present the prompt there, an infinite
        // wait leaves an orphan holding the pidfile while the parent gives up.
        dispatch_semaphore_wait(done, dispatch_time(DISPATCH_TIME_NOW, 120ll * NSEC_PER_SEC));
        return granted;
    }
}
