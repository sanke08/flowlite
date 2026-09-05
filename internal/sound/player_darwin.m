#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>

// macOS cue playback through AVAudioPlayer — the same system-managed path
// afplay uses. Each cue becomes one preloaded player (its samples wrapped in
// an in-memory float32 WAV), so Play is just [player play] on a serial queue:
// no device of our own, no real-time callback, nothing for the Go runtime to
// be late for. Independent players let cues overlap (Stop under Working).

static NSMutableDictionary<NSNumber *, AVAudioPlayer *> *players = nil;
static dispatch_queue_t queue = nil;

static void ensure(void) {
    if (!queue) {
        queue = dispatch_queue_create("flowlite.sound", DISPATCH_QUEUE_SERIAL);
        players = [NSMutableDictionary new];
    }
}

static NSData *wavFloat32(const float *samples, int n, int rate) {
    uint32_t dataBytes = (uint32_t)n * 4;
    NSMutableData *d = [NSMutableData dataWithCapacity:44 + dataBytes];
    uint32_t u32; uint16_t u16;
#define W32(v) u32 = (v); [d appendBytes:&u32 length:4]
#define W16(v) u16 = (v); [d appendBytes:&u16 length:2]
    [d appendBytes:"RIFF" length:4]; W32(36 + dataBytes);
    [d appendBytes:"WAVE" length:4];
    [d appendBytes:"fmt " length:4]; W32(16);
    W16(3);            // WAVE_FORMAT_IEEE_FLOAT
    W16(1);            // mono
    W32((uint32_t)rate);
    W32((uint32_t)rate * 4);
    W16(4);            // block align
    W16(32);           // bits
    [d appendBytes:"data" length:4]; W32(dataBytes);
    [d appendBytes:samples length:dataBytes];
#undef W32
#undef W16
    return d;
}

// flowlite_sound_load builds and prepares the player for cue idx. Returns 0
// on success, otherwise the NSError code.
int flowlite_sound_load(int idx, const float *samples, int n, int rate) {
    @autoreleasepool {
        ensure();
        NSError *err = nil;
        AVAudioPlayer *p = [[AVAudioPlayer alloc] initWithData:wavFloat32(samples, n, rate) error:&err];
        if (!p) return err ? (int)err.code : -1;
        p.volume = 1.0;
        [p prepareToPlay];
        __block int rc = 0;
        dispatch_sync(queue, ^{ players[@(idx)] = p; });
        return rc;
    }
}

// flowlite_sound_play (re)starts cue idx from the beginning.
void flowlite_sound_play(int idx) {
    if (!queue) return;
    dispatch_async(queue, ^{
        AVAudioPlayer *p = players[@(idx)];
        if (!p) return;
        if (p.playing) [p pause];
        p.currentTime = 0;
        [p play];
    });
}

// flowlite_sound_close stops and drops every player.
void flowlite_sound_close(void) {
    if (!queue) return;
    dispatch_sync(queue, ^{
        for (AVAudioPlayer *p in players.allValues) [p stop];
        [players removeAllObjects];
    });
}
