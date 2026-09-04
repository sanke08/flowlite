#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

// A small capsule with no text: a waveform while listening, a spinner while
// transcribing, a check (or ×) when done. Every transition is time-based and
// eased, so it reads as one continuous object changing shape.

#define PILL_W      100.0
#define PILL_H       30.0
#define RADIUS       15.0
#define BOTTOM_GAP   64.0
#define BARS          9
#define BAR_W         3.0
#define BAR_GAP       3.0
#define FPS          60.0

enum { ST_HIDDEN, ST_LISTENING, ST_TRANSCRIBING, ST_PASTED, ST_CANCELLED, ST_ERROR };

static double now_s(void) { return CACurrentMediaTime(); }
static double clamp01(double v) { return v < 0 ? 0 : (v > 1 ? 1 : v); }
static double easeOut(double t) { t = clamp01(t); return 1 - pow(1 - t, 3); }
static double easeInOut(double t) { t = clamp01(t); return t < 0.5 ? 4 * t * t * t : 1 - pow(-2 * t + 2, 3) / 2; }

@interface FLPillView : NSView {
@public
    int    state;
    double stateAt;      // when the state last changed
    double level;        // smoothed mic level
    double target;       // latest mic level
    double bars[BARS];   // per-bar smoothed heights, 0..1
}
@end

@implementation FLPillView

- (BOOL)isOpaque { return NO; }

- (void)drawRect:(NSRect)dirty {
    (void)dirty;
    double t  = now_s();
    double st = t - stateAt;
    NSRect b  = self.bounds;
    double cx = NSMidX(b), cy = NSMidY(b);

    // Body
    NSBezierPath *body = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(b, 0.5, 0.5)
                                                         xRadius:RADIUS yRadius:RADIUS];
    [[NSColor colorWithCalibratedRed:0.09 green:0.09 blue:0.11 alpha:0.95] setFill];
    [body fill];
    [[NSColor colorWithCalibratedWhite:1.0 alpha:0.10] setStroke];
    [body setLineWidth:1.0];
    [body stroke];

    // Layer opacities driven by state + time-in-state
    double barsA = 0, spinA = 0, markA = 0;
    switch (state) {
        case ST_LISTENING:    barsA = easeOut(st / 0.18); break;
        case ST_TRANSCRIBING: barsA = 1 - easeInOut(st / 0.22);
                              spinA = easeOut((st - 0.08) / 0.22); break;
        case ST_PASTED:
        case ST_CANCELLED:
        case ST_ERROR:        spinA = 1 - easeInOut(st / 0.15);
                              markA = easeOut((st - 0.05) / 0.30); break;
        default: break;
    }

    // ---- waveform -----------------------------------------------------
    level += (target - level) * 0.5;
    double center = (BARS - 1) / 2.0;
    for (int i = 0; i < BARS; i++) {
        double env = 1.0 - 0.55 * pow(fabs(i - center) / center, 1.6);   // centre tallest
        double wob = 0.72 + 0.28 * sin(t * (6.5 + i * 0.9) + i * 1.7);    // breathes even when steady
        double desired = clamp01(level * 1.9) * env * wob;
        bars[i] += (desired - bars[i]) * 0.35;
    }
    if (barsA > 0.001) {
        double span = BARS * (BAR_W + BAR_GAP) - BAR_GAP;
        double x = cx - span / 2;
        double maxH = PILL_H - 12;
        [[NSColor colorWithCalibratedWhite:1.0 alpha:0.92 * barsA] setFill];
        for (int i = 0; i < BARS; i++) {
            double h = (3.0 + bars[i] * (maxH - 3.0)) * (0.15 + 0.85 * barsA);
            NSRect br = NSMakeRect(x, cy - h / 2, BAR_W, h);
            [[NSBezierPath bezierPathWithRoundedRect:br xRadius:1.5 yRadius:1.5] fill];
            x += BAR_W + BAR_GAP;
        }
    }

    // ---- spinner ------------------------------------------------------
    if (spinA > 0.001) {
        double scale = 0.7 + 0.3 * spinA;
        double r = 7.5 * scale;
        double start = fmod(-t * 396.0, 360.0);          // 1.1 rev/s
        NSBezierPath *arc = [NSBezierPath bezierPath];
        [arc appendBezierPathWithArcWithCenter:NSMakePoint(cx, cy) radius:r
                                    startAngle:start endAngle:start - 270 clockwise:YES];
        [arc setLineWidth:2.4];
        [arc setLineCapStyle:NSLineCapStyleRound];
        [[NSColor colorWithCalibratedRed:0.47 green:0.67 blue:1.0 alpha:spinA] setStroke];
        [arc stroke];
    }

    // ---- check / cross ------------------------------------------------
    if (markA > 0.001) {
        NSBezierPath *p = [NSBezierPath bezierPath];
        [p setLineWidth:2.5];
        [p setLineCapStyle:NSLineCapStyleRound];
        [p setLineJoinStyle:NSLineJoinStyleRound];
        NSColor *c;
        if (state == ST_PASTED) {
            c = [NSColor colorWithCalibratedRed:0.24 green:0.81 blue:0.56 alpha:1];
            // Two segments drawn in sequence: short down-stroke, long up-stroke.
            NSPoint a = NSMakePoint(cx - 6.5, cy + 0.5);
            NSPoint m = NSMakePoint(cx - 2.0, cy - 4.0);
            NSPoint z = NSMakePoint(cx + 6.5, cy + 5.0);
            double s1 = clamp01(markA / 0.4), s2 = clamp01((markA - 0.4) / 0.6);
            [p moveToPoint:a];
            [p lineToPoint:NSMakePoint(a.x + (m.x - a.x) * s1, a.y + (m.y - a.y) * s1)];
            if (s2 > 0) [p lineToPoint:NSMakePoint(m.x + (z.x - m.x) * s2, m.y + (z.y - m.y) * s2)];
        } else {
            c = (state == ST_ERROR)
                ? [NSColor colorWithCalibratedRed:1.0 green:0.36 blue:0.36 alpha:1]
                : [NSColor colorWithCalibratedWhite:0.62 alpha:1];
            double k = 5.0 * markA;
            [p moveToPoint:NSMakePoint(cx - k, cy - k)];
            [p lineToPoint:NSMakePoint(cx + k, cy + k)];
            [p moveToPoint:NSMakePoint(cx - k, cy + k)];
            [p lineToPoint:NSMakePoint(cx + k, cy - k)];
        }
        [c setStroke];
        [p stroke];
    }
}
@end

// ---- panel -----------------------------------------------------------------

static NSPanel    *panel = nil;
static FLPillView *view  = nil;
static NSTimer    *timer = nil;
static double      shownAt = 0;   // fade-in start
static double      hideAt  = 0;   // >0 while fading out
static NSPoint     baseOrigin;

static void ensurePanel(void) {
    if (panel) return;
    NSRect frame = NSMakeRect(0, 0, PILL_W, PILL_H);
    panel = [[NSPanel alloc] initWithContentRect:frame
                                       styleMask:(NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel)
                                         backing:NSBackingStoreBuffered
                                           defer:NO];
    [panel setLevel:NSStatusWindowLevel];
    [panel setOpaque:NO];
    [panel setBackgroundColor:[NSColor clearColor]];
    [panel setHasShadow:YES];
    [panel setIgnoresMouseEvents:YES];
    [panel setHidesOnDeactivate:NO];
    [panel setCollectionBehavior:(NSWindowCollectionBehaviorCanJoinAllSpaces
                                  | NSWindowCollectionBehaviorStationary
                                  | NSWindowCollectionBehaviorFullScreenAuxiliary)];
    view = [[FLPillView alloc] initWithFrame:frame];
    [panel setContentView:view];
}

// Bottom-centre of whichever screen holds the mouse.
static void reposition(void) {
    NSPoint mouse = [NSEvent mouseLocation];
    NSScreen *target = [NSScreen mainScreen];
    for (NSScreen *s in [NSScreen screens]) {
        if (NSMouseInRect(mouse, [s frame], NO)) { target = s; break; }
    }
    NSRect vf = [target visibleFrame];
    baseOrigin = NSMakePoint(NSMidX(vf) - PILL_W / 2, NSMinY(vf) + BOTTOM_GAP);
}

static void stopTimer(void) {
    if (!timer) return;
    [timer invalidate];
    timer = nil;
}

static void tick(void) {
    double t = now_s();
    double alpha = easeOut((t - shownAt) / 0.16);
    double lift  = 6.0 * (1.0 - alpha);                 // slides up as it appears
    if (hideAt > 0) {
        double f = easeInOut((t - hideAt) / 0.24);
        alpha *= (1.0 - f);
        lift  -= 4.0 * f;                               // sinks slightly as it goes
        if (f >= 1.0) {
            [panel orderOut:nil];
            view->state = ST_HIDDEN;
            hideAt = 0;
            stopTimer();
            return;
        }
    }
    [panel setAlphaValue:alpha];
    [panel setFrameOrigin:NSMakePoint(baseOrigin.x, baseOrigin.y + lift)];
    [view setNeedsDisplay:YES];
}

static void startTimer(void) {
    if (timer) return;
    timer = [NSTimer scheduledTimerWithTimeInterval:(1.0 / FPS)
                                            repeats:YES
                                              block:^(NSTimer *tm) { (void)tm; tick(); }];
    [[NSRunLoop mainRunLoop] addTimer:timer forMode:NSRunLoopCommonModes];
}

void flowlite_overlay_show(int state, const char *text) {
    (void)text;
    @autoreleasepool {
        ensurePanel();
        BOOL fresh = (view->state == ST_HIDDEN) || hideAt > 0;
        double t = now_s();
        view->state = state;
        view->stateAt = t;
        hideAt = 0;
        if (fresh) {
            shownAt = t;
            view->level = view->target = 0;
            for (int i = 0; i < BARS; i++) view->bars[i] = 0;
            reposition();
            [panel setAlphaValue:0];
            [panel setFrameOrigin:baseOrigin];
            [panel orderFrontRegardless];
        }
        startTimer();
        [view setNeedsDisplay:YES];
    }
}

void flowlite_overlay_set_state(int state, const char *text) {
    @autoreleasepool {
        if (!panel || view->state == ST_HIDDEN) { flowlite_overlay_show(state, text); return; }
        view->state = state;
        view->stateAt = now_s();
        hideAt = 0;
        startTimer();
        [view setNeedsDisplay:YES];
    }
}

void flowlite_overlay_set_level(float level) {
    if (!view) return;
    view->target = clamp01(level);
}

void flowlite_overlay_hide(void) {
    @autoreleasepool {
        if (!panel || view->state == ST_HIDDEN || hideAt > 0) return;
        hideAt = now_s();
        startTimer();
    }
}

bool flowlite_overlay_snapshot(const char *path) {
    @autoreleasepool {
        ensurePanel();
        NSBitmapImageRep *rep = [view bitmapImageRepForCachingDisplayInRect:[view bounds]];
        if (!rep) return false;
        [view cacheDisplayInRect:[view bounds] toBitmapImageRep:rep];
        NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        return [png writeToFile:[NSString stringWithUTF8String:path] atomically:YES];
    }
}
