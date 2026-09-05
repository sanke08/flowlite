#import <Cocoa/Cocoa.h>
#include <stdint.h>

extern void flowliteMainloopCall(uintptr_t handle);
extern void flowliteMainloopWake(void);

void flowlite_mainloop_prepare(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        // Accessory: no Dock icon, no menu bar takeover, but windows allowed.
        [NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
        // Closing the lid suspends whatever CoreAudio device an open stream
        // was bound to; it needs to be rebuilt once the machine actually
        // wakes, not just re-enabled where it was left off.
        [[[NSWorkspace sharedWorkspace] notificationCenter]
            addObserverForName:NSWorkspaceDidWakeNotification
                        object:nil
                         queue:[NSOperationQueue mainQueue]
                    usingBlock:^(NSNotification *n) {
                        (void)n;
                        flowliteMainloopWake();
                    }];
    }
}

void flowlite_mainloop_run(void) {
    @autoreleasepool {
        [NSApp run];
    }
}

void flowlite_mainloop_stop(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [NSApp stop:nil];
        // -stop: only takes effect once the loop processes another event.
        NSEvent *wake = [NSEvent otherEventWithType:NSEventTypeApplicationDefined
                                           location:NSZeroPoint
                                      modifierFlags:0
                                          timestamp:0
                                       windowNumber:0
                                            context:nil
                                            subtype:0
                                              data1:0
                                              data2:0];
        [NSApp postEvent:wake atStart:YES];
    });
}

void flowlite_mainloop_dispatch(uintptr_t handle) {
    dispatch_async(dispatch_get_main_queue(), ^{
        @autoreleasepool {
            flowliteMainloopCall(handle);
        }
    });
}
