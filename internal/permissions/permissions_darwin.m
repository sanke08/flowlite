#import <ApplicationServices/ApplicationServices.h>
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
