#import <AVFoundation/AVFoundation.h>
#import <Foundation/Foundation.h>

// macOS cue playback through AVAudioPlayer — the same system-managed path
// afplay uses. Each cue becomes one preloaded player (its samples wrapped in
// an in-memory float32 WAV), so Play is just [player play] on a serial queue:
// no device of our own, no real-time callback, nothing for the Go runtime to
// be late for. Independent players let cues overlap (Stop under Working).

// Each cue gets a small pool of identical players used in rotation. One
// player per cue is not enough for the Working ticker: at 65ms between
// ticks the next Play lands before the previous [play] has even started
// producing sound, and pausing/rewinding/restarting that same instance that
// fast yields silence. With a pool, every tick starts an idle instance.
#define POOL 4
static NSMutableDictionary<NSNumber *, NSMutableArray<AVAudioPlayer *> *> *players = nil;
static NSMutableDictionary<NSNumber *, NSNumber *> *next = nil;
static dispatch_queue_t queue = nil;

static void ensure(void) {
    if (!queue) {
        queue = dispatch_queue_create("flowlite.sound", DISPATCH_QUEUE_SERIAL);
        players = [NSMutableDictionary new];
        next = [NSMutableDictionary new];
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

// flowlite_sound_load builds and prepares the players for cue idx. Returns 0
// on success, otherwise the NSError code.
int flowlite_sound_load(int idx, const float *samples, int n, int rate) {
    @autoreleasepool {
        ensure();
        NSData *wav = wavFloat32(samples, n, rate);
        NSMutableArray<AVAudioPlayer *> *pool = [NSMutableArray arrayWithCapacity:POOL];
        for (int i = 0; i < POOL; i++) {
            NSError *err = nil;
            AVAudioPlayer *p = [[AVAudioPlayer alloc] initWithData:wav error:&err];
            if (!p) return err ? (int)err.code : -1;
            p.volume = 1.0;
            [p prepareToPlay];
            [pool addObject:p];
        }
        dispatch_sync(queue, ^{ players[@(idx)] = pool; next[@(idx)] = @0; });
        return 0;
    }
}

// flowlite_sound_play starts cue idx on the next idle player in its pool.
void flowlite_sound_play(int idx) {
    if (!queue) return;
    dispatch_async(queue, ^{
        NSMutableArray<AVAudioPlayer *> *pool = players[@(idx)];
        if (!pool) return;
        int i = next[@(idx)].intValue;
        next[@(idx)] = @((i + 1) % POOL);
        AVAudioPlayer *p = pool[i];
        if (p.playing) [p stop];
        p.currentTime = 0;
        [p play];
    });
}

// flowlite_sound_close stops and drops every player.
void flowlite_sound_close(void) {
    if (!queue) return;
    dispatch_sync(queue, ^{
        for (NSArray<AVAudioPlayer *> *pool in players.allValues)
            for (AVAudioPlayer *p in pool) [p stop];
        [players removeAllObjects];
        [next removeAllObjects];
    });
}
