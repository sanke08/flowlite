#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

// A small black capsule with no text, ever. It has exactly three looks:
//   recording  — a centre-weighted waveform driven by the mic level
//   processing — the bars settle into short equal stubs and a soft band of
//                light sweeps back and forth across them
//   failed     — the stubs turn red, pulse twice, then the pill fades away
// Success and cancel draw nothing new: the pill simply fades out.
//
// The pill sits centred on one edge of the screen (bottom by default, or top,
// left, right), EDGE_GAP points in from the *physical* edge on every side. At
// the bottom that overlaps the Dock area on purpose; at the top it clears the
// menu bar and the camera notch first, then that same gap. On the left/right
// edges the capsule stands upright and the bars run horizontally, stacked top
// to bottom.
// Every transition is time-based and eased, so it reads as one continuous
// object changing shape.

#define PILL_LONG   100.0
#define PILL_SHORT   30.0
#define RADIUS       15.0
#define EDGE_GAP     20.0
#define BARS          9
#define BAR_W         3.0
#define BAR_GAP       3.0
#define FPS          60.0
#define FADE_IN       0.16
#define FADE_OUT      0.20
#define ERROR_HOLD    0.70   // two red pulses, then fade

enum { ST_HIDDEN, ST_LISTENING, ST_TRANSCRIBING, ST_PASTED, ST_CANCELLED, ST_ERROR };
enum { POS_BOTTOM, POS_TOP, POS_LEFT, POS_RIGHT };

static int position = POS_BOTTOM;
static BOOL vertical(void) { return position == POS_LEFT || position == POS_RIGHT; }
// Every edge keeps the same gap in from the physical edge — on the bottom
// that is what lets the pill ride over the Dock; on the sides it is just
// consistent with that rather than sitting flush against the screen.
static double edgeGap(void) { return EDGE_GAP; }

static double now_s(void) { return CACurrentMediaTime(); }
static double clamp01(double v) { return v < 0 ? 0 : (v > 1 ? 1 : v); }
static double mix(double a, double b, double k) { return a + (b - a) * k; }
static double easeOut(double t) { t = clamp01(t); return 1 - pow(1 - t, 3); }
static double easeInOut(double t) { t = clamp01(t); return t < 0.5 ? 4 * t * t * t : 1 - pow(-2 * t + 2, 3) / 2; }
// Frame-rate independent exponential approach: v moves toward want with time constant tau.
static void approach(double *v, double want, double dt, double tau) { *v += (want - *v) * (1 - exp(-dt / tau)); }

@interface FLPillView : NSView {
@public
    int    state;
    double stateAt;      // when the state last changed
    double appearAt;     // when the pill was last shown from hidden
    double lastTick;     // previous frame time, for dt
    double level;        // smoothed mic level
    double target;       // latest mic level
    double bars[BARS];   // per-bar smoothed heights, 0..1
    double collapse;     // 0 = live waveform, 1 = equal stubs
    double red;          // 0 = white, 1 = failure red
    double shimmer;      // 0 = flat, 1 = sweeping band
}
@end

@implementation FLPillView

- (BOOL)isOpaque { return NO; }

- (void)drawRect:(NSRect)dirty {
    (void)dirty;
    double t  = now_s();
    double dt = lastTick > 0 ? fmin(t - lastTick, 0.1) : 1.0 / FPS;
    lastTick  = t;
    double st = t - stateAt;
    NSRect b  = self.bounds;
    double cx = NSMidX(b), cy = NSMidY(b);
    BOOL   vert = b.size.height > b.size.width;

    // ---- body -----------------------------------------------------------
    NSBezierPath *body = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(b, 0.5, 0.5)
                                                         xRadius:RADIUS yRadius:RADIUS];
    [[NSColor colorWithCalibratedRed:0 green:0 blue:0 alpha:0.94] setFill];
    [body fill];
    [[NSColor colorWithCalibratedWhite:1.0 alpha:0.045] setStroke];
    [body setLineWidth:1.0];
    [body stroke];

    // ---- shape blend targets ------------------------------------------
    approach(&collapse, state == ST_LISTENING ? 0 : 1, dt, 0.10);
    approach(&red,      state == ST_ERROR ? 1 : 0,     dt, 0.06);
    approach(&shimmer,  state == ST_TRANSCRIBING ? 1 : 0, dt, 0.12);

    // ---- live waveform model (runs always so hand-offs are seamless) ---
    level += (target - level) * 0.5;
    double center = (BARS - 1) / 2.0;
    for (int i = 0; i < BARS; i++) {
        double env = 1.0 - 0.55 * pow(fabs(i - center) / center, 1.6);   // centre tallest
        double wob = 0.72 + 0.28 * sin(t * (6.5 + i * 0.9) + i * 1.7);    // breathes even when steady
        double desired = clamp01(level * 1.9) * env * wob;
        bars[i] += (desired - bars[i]) * 0.35;
    }

    double barsA = easeOut((t - appearAt) / 0.18);
    if (barsA <= 0.001) return;

    // Sweep position for the processing shimmer: 0..1 along the bars,
    // starting at one end when processing begins, back and forth at ~0.7 Hz.
    double sweep = 0.5 - 0.5 * cos(2 * M_PI * st / 1.4);
    // Failure: two bright pulses over ERROR_HOLD, bright at 0, .35, .70.
    double pulse = 0.45 + 0.55 * (0.5 + 0.5 * cos(2 * M_PI * st / (ERROR_HOLD / 2)));

    double maxL = PILL_SHORT - 12;
    double span = BARS * (BAR_W + BAR_GAP) - BAR_GAP;
    double p = (vert ? cy : cx) - span / 2;
    for (int i = 0; i < BARS; i++) {
        double u    = (double)i / (BARS - 1);
        double glow = exp(-pow((u - sweep) / 0.16, 2)) * shimmer;

        double waveL = 3.0 + bars[i] * (maxL - 3.0);
        double stubL = 5.0 + 2.0 * glow + 3.0 * pulse * red;
        double len   = mix(waveL, stubL, collapse) * (0.15 + 0.85 * barsA);

        double a = mix(0.92, 0.30 + 0.62 * glow, shimmer);   // shimmer band
        a = mix(a, pulse, red);                               // red pulses
        NSColor *c = [NSColor colorWithCalibratedRed:1.0
                                               green:mix(1.0, 0.36, red)
                                                blue:mix(1.0, 0.36, red)
                                               alpha:a * barsA];
        [c setFill];
        NSRect r = vert ? NSMakeRect(cx - len / 2, p, len, BAR_W)
                        : NSMakeRect(p, cy - len / 2, BAR_W, len);
        [[NSBezierPath bezierPathWithRoundedRect:r xRadius:1.5 yRadius:1.5] fill];
        p += BAR_W + BAR_GAP;
    }
}
@end

// ---- panel -----------------------------------------------------------------

static NSPanel    *panel = nil;
static FLPillView *view  = nil;
static NSTimer    *timer = nil;
static double      shownAt = 0;   // fade-in start
static double      hideAt  = 0;   // >0 while fading out (may be in the future)
static NSPoint     baseOrigin;
static NSPoint     inward;        // unit vector pointing away from the screen edge

static double      checkAt = 0;   // >0: verify the Space assignment at this time

// The traits that make the pill a passive overlay on every Space. Re-applied
// to every panel we build, and re-asserted each time the pill is shown.
static void applyTraits(NSPanel *p) {
    [p setLevel:NSStatusWindowLevel];
    [p setOpaque:NO];
    [p setBackgroundColor:[NSColor clearColor]];
    [p setHasShadow:YES];
    [p setIgnoresMouseEvents:YES];
    [p setHidesOnDeactivate:NO];
    [p setReleasedWhenClosed:NO];
    [p setCollectionBehavior:(NSWindowCollectionBehaviorCanJoinAllSpaces
                              | NSWindowCollectionBehaviorStationary
                              | NSWindowCollectionBehaviorFullScreenAuxiliary)];
}

// Drop the window but keep the view: the pill's animation state lives in the
// view, so a replacement window picks up mid-gesture without a flicker. The
// timer is left alone — tick may be the caller.
static void discardPanel(void) {
    if (!panel) return;
    [panel orderOut:nil];
    [view removeFromSuperview];
    panel = nil;
}

// A sleep/wake cycle or a display change can leave the window server holding a
// stale Space assignment for the panel, and canJoinAllSpaces then stops
// following the user — the pill draws at full alpha on a Space nobody is
// looking at. Throw the window away while it is hidden; the next show builds a
// fresh one, and a fresh window always lands on the active Space.
static void installObservers(void) {
    static BOOL installed = NO;
    if (installed) return;
    installed = YES;
    void (^refresh)(NSNotification *) = ^(NSNotification *n) {
        (void)n;
        if (!view || view->state == ST_HIDDEN) discardPanel();
    };
    [[[NSWorkspace sharedWorkspace] notificationCenter]
        addObserverForName:NSWorkspaceDidWakeNotification
                    object:nil
                     queue:[NSOperationQueue mainQueue]
                usingBlock:refresh];
    [[NSNotificationCenter defaultCenter]
        addObserverForName:NSApplicationDidChangeScreenParametersNotification
                    object:nil
                     queue:[NSOperationQueue mainQueue]
                usingBlock:refresh];
}

static void ensurePanel(void) {
    if (panel) return;
    NSRect frame = NSMakeRect(0, 0, PILL_LONG, PILL_SHORT);
    panel = [[NSPanel alloc] initWithContentRect:frame
                                       styleMask:(NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel)
                                         backing:NSBackingStoreBuffered
                                           defer:NO];
    applyTraits(panel);
    if (!view) view = [[FLPillView alloc] initWithFrame:frame];
    [panel setContentView:view];
    installObservers();
}

// How far down the top of the screen is unusable. On a Mac with a camera
// notch the top centre is exactly where the housing sits — and the pill is
// centred, so it would land right behind it. The menu bar and the notch are
// usually the same height, but the menu bar can be set to auto-hide while the
// notch is physical and never goes away, so take whichever reaches further.
static double topInset(NSScreen *s) {
    double menuBar = NSMaxY([s frame]) - NSMaxY([s visibleFrame]);
    return fmax(menuBar, [s safeAreaInsets].top);
}

// Centred on the chosen edge of whichever screen holds the mouse, EDGE_GAP
// in from the physical edge (not the visibleFrame — the Dock does not push it).
static void reposition(void) {
    NSPoint mouse = [NSEvent mouseLocation];
    NSScreen *target = [NSScreen mainScreen];
    for (NSScreen *s in [NSScreen screens]) {
        if (NSMouseInRect(mouse, [s frame], NO)) { target = s; break; }
    }
    NSRect sf = [target frame];
    double w = vertical() ? PILL_SHORT : PILL_LONG;
    double h = vertical() ? PILL_LONG : PILL_SHORT;
    switch (position) {
        case POS_TOP:
            baseOrigin = NSMakePoint(NSMidX(sf) - w / 2, NSMaxY(sf) - topInset(target) - edgeGap() - h);
            inward = NSMakePoint(0, -1);
            break;
        case POS_LEFT:
            baseOrigin = NSMakePoint(NSMinX(sf) + edgeGap(), NSMidY(sf) - h / 2);
            inward = NSMakePoint(1, 0);
            break;
        case POS_RIGHT:
            baseOrigin = NSMakePoint(NSMaxX(sf) - edgeGap() - w, NSMidY(sf) - h / 2);
            inward = NSMakePoint(-1, 0);
            break;
        default:
            baseOrigin = NSMakePoint(NSMidX(sf) - w / 2, NSMinY(sf) + edgeGap());
            inward = NSMakePoint(0, 1);
            break;
    }
    [panel setFrame:NSMakeRect(baseOrigin.x, baseOrigin.y, w, h) display:NO];
}

// Replace a window the window server has stranded on the wrong Space,
// carrying the current alpha and the view across so the pill just keeps going.
static void rebuildPanel(void) {
    double alpha = panel ? [panel alphaValue] : 0;
    discardPanel();
    ensurePanel();
    reposition();
    [panel setAlphaValue:alpha];
    [panel orderFrontRegardless];
}

static void stopTimer(void) {
    if (!timer) return;
    [timer invalidate];
    timer = nil;
}

static void tick(void) {
    double t = now_s();
    // Shortly after a show, make sure the window really did land where the
    // user is looking. If it did not, a fresh one will.
    if (checkAt > 0 && t >= checkAt) {
        checkAt = 0;
        if (panel && ![panel isOnActiveSpace]) rebuildPanel();
    }
    if (view->state == ST_ERROR && hideAt == 0 && t - view->stateAt >= ERROR_HOLD) hideAt = t;

    double alpha = easeOut((t - shownAt) / FADE_IN);
    double slide = -6.0 * (1.0 - alpha);                // arrives from the edge
    if (hideAt > 0) {
        double f = easeInOut((t - hideAt) / FADE_OUT);
        alpha *= (1.0 - f);
        slide -= 4.0 * f;                               // drifts back toward it
        if (f >= 1.0) {
            [panel orderOut:nil];
            view->state = ST_HIDDEN;
            hideAt = 0;
            checkAt = 0;
            stopTimer();
            return;
        }
    }
    [panel setAlphaValue:alpha];
    [panel setFrameOrigin:NSMakePoint(baseOrigin.x + inward.x * slide, baseOrigin.y + inward.y * slide)];
    [view setNeedsDisplay:YES];
}

static void startTimer(void) {
    if (timer) return;
    timer = [NSTimer scheduledTimerWithTimeInterval:(1.0 / FPS)
                                            repeats:YES
                                              block:^(NSTimer *tm) { (void)tm; tick(); }];
    [[NSRunLoop mainRunLoop] addTimer:timer forMode:NSRunLoopCommonModes];
}

static bool terminal(int state) { return state == ST_PASTED || state == ST_CANCELLED; }
void flowlite_overlay_hide(void);

void flowlite_overlay_set_position(int pos) {
    if (pos < POS_BOTTOM || pos > POS_RIGHT) pos = POS_BOTTOM;
    position = pos;
}

void flowlite_overlay_show(int state, const char *text) {
    (void)text;
    @autoreleasepool {
        ensurePanel();
        BOOL fresh = (view->state == ST_HIDDEN) || hideAt > 0;
        double t = now_s();
        if (fresh && terminal(state)) {
            // Nothing to show: success/cancel have no look of their own.
            if (view->state != ST_HIDDEN) flowlite_overlay_hide();
            return;
        }
        view->state = state;
        view->stateAt = t;
        hideAt = 0;
        if (fresh) {
            shownAt = t;
            view->appearAt = t;
            view->lastTick = 0;
            view->level = view->target = 0;
            view->collapse = (state == ST_LISTENING) ? 0 : 1;
            view->red = view->shimmer = 0;
            for (int i = 0; i < BARS; i++) view->bars[i] = 0;
            reposition();
            applyTraits(panel);
            [panel setAlphaValue:0];
            [panel orderFrontRegardless];
            checkAt = t + 0.12;
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
        if (terminal(state)) hideAt = view->stateAt;    // fade out right away
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
        double t = now_s();
        // Let a failure finish its two pulses before it goes.
        hideAt = (view->state == ST_ERROR) ? fmax(t, view->stateAt + ERROR_HOLD) : t;
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
