#import <Cocoa/Cocoa.h>
#import <QuartzCore/QuartzCore.h>

// Called back into Go when the user acts on the history panel (see
// overlay_darwin.go's flowliteHistoryPick/flowliteHistoryClosed).
extern void flowliteHistoryPick(int index);
extern void flowliteHistoryClosed(void);

// A small black capsule with no text, ever — with one narrow, deliberate
// exception: a 1-3 word status label for the terminal Error state, since the
// daemon runs detached with no terminal to explain a failure any other way.
// It has exactly three looks:
//   recording  — a centre-weighted waveform driven by the mic level
//   processing — the bars settle into short equal stubs and a soft band of
//                light sweeps back and forth across them
//   failed     — the stubs turn red, pulse twice, then the pill fades away
// Success and cancel draw nothing new: the pill simply fades out, wordlessly
// — there is nothing to explain when there was simply nothing to transcribe.
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
#define FADE_IN       0.14
#define FADE_OUT      0.24
#define ERROR_HOLD    0.70   // two red pulses, then fade

// The pill's arrival and departure are two coupled springs, not eased curves,
// so it reads as something soft settling onto the screen rather than a panel
// sliding in:
//   slide — the offset along `inward` from the resting spot. Starts 14pt
//           toward the edge and glides in over ~0.35s.
//   pop   — a scale factor around the pill's centre, from 0.80 to 1. The part
//           of any overshoot above 1 is not drawn as growth (the window is
//           exactly pill-sized, it would clip) but as a slight *squash*
//           across the direction of motion, so it lands softly.
//   stretch — while moving fast it elongates a little along the motion axis
//           and thins across it, in proportion to slide velocity: liquid,
//           not a rigid sticker.
// Both springs sit just under critical damping (zeta ≈ 0.8): a visible
// overshoot means the pill stops dead at the peak and reverses, and the eye
// reads that as a hitch ("comes, sticks, comes"). Near-critical keeps one
// continuous deceleration with only a hint of settle at the end.
// Alpha stays a plain ease: opacity does not read as springy, shape does.
#define SLIDE_STIFFNESS  300.0
#define SLIDE_DAMPING     28.0
#define SLIDE_TRAVEL      14.0   // pt the pill starts/ends toward the edge
#define POP_STIFFNESS    300.0
#define POP_DAMPING       26.0
#define POP_FROM           0.80  // scale the pill appears/leaves at
#define POP_SQUASH         0.80  // how much of the >1 overshoot becomes squash
#define STRETCH_GAIN       0.0006 // scale per pt/s of slide velocity
#define STRETCH_MAX        0.06

// Shared between the panel's tick() (which integrates the springs) and
// FLPillView's drawRect (which applies them as a transform). Tentative
// definitions; the panel section below defines the rest of the layout state.
static double  slidePos  = 0, slideVel = 0;
static double  popPos    = 1, popVel   = 0;
static NSPoint inward;

// The label only ever appears for the terminal Error state, and only grows
// the pill's own footprint enough to hold a short (1-3 word) status without
// wrapping — it is not a place for real text.
#define LABEL_FONT_SIZE  11.0
#define LABEL_SIDE_PAD   12.0    // horizontal padding around the label text
#define LABEL_THICK      16.0   // extra room made below the bars, pill lying flat
#define LABEL_MAX_TEXT  220.0   // widest the label's own text may lay out before truncating

// The history panel: a "morph" of the same black capsule, grown large enough
// to hold a scrollable list of past transcripts. It reuses the pill's own
// RADIUS/body colour so it reads as the same object changing shape, not a
// second surface — see flowlite_overlay_show_history below.
#define HIST_W        340.0
#define HIST_H        420.0
#define HIST_ROW_H     28.0
#define HIST_ROW_VPAD   4.0    // top/bottom breathing room inside a (possibly
                               // wrapped, multi-line) row, see FLHistoryDataSource
#define HIST_ROW_MAXLINES 3    // cap on how tall one preview may wrap before
                               // truncating, so one long transcript can't eat
                               // the whole panel
#define HIST_TIME_W    40.0    // fixed width for the "HH:MM" time label, see
                               // FLHistoryDataSource — every timestamp is the
                               // same format/length, so a measured width buys
                               // nothing over a constant
#define HIST_TIME_GAP   8.0    // gap between the time label's trailing edge
                               // and the wrapping preview field's leading edge
#define HIST_PAD_TOP   10.0
#define HIST_PAD_BOTTOM 10.0
#define HIST_PAD_SIDE   8.0
#define HIST_ANIM_DUR   0.28

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
    NSString *label;     // status text, shown only for ST_ERROR
    BOOL   historyOpen;  // YES while the history panel owns the body: draw
                         // just the flat rounded black backdrop, nothing else
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
    BOOL   vert = b.size.height > b.size.width;
    BOOL   showLabel = (state == ST_ERROR) && label.length > 0;

    // The label (only shown for Error) claims a strip of the
    // pill's own bounds — below the bars when the pill lies flat, beside
    // them when it stands upright — so the bars themselves keep their
    // original size and centring regardless of whether the pill grew.
    NSRect core = b;
    if (showLabel) {
        if (vert) {
            core.size.width = PILL_SHORT;
        } else {
            core.origin.y   = NSMaxY(b) - PILL_SHORT;
            core.size.height = PILL_SHORT;
        }
    }
    double cx = NSMidX(core), cy = NSMidY(core);

    // ---- liquid pop transform ------------------------------------------
    // Scale around the pill's own centre. Along the motion axis: the pop
    // (capped at 1) plus velocity stretch. Across it: the pop minus the
    // squash that the >1 overshoot turns into, minus the same stretch.
    // Neither axis ever exceeds 1, so nothing is clipped by the window.
    //
    // Skipped while the history panel is open: the pop spring is only
    // integrated by tick(), which the history morph stops, so popPos is
    // frozen at whatever the last pill appearance left it — POP_FROM (0.8)
    // after a fade-out. Applying that here would draw the backdrop at 80%
    // of the panel, leaving the table's edges (time column, line ends) over
    // bare desktop and the whole thing reading as translucent.
    if (!historyOpen) {
        double over    = fmax(popPos - 1.0, 0.0);
        double base    = fmin(popPos, 1.0);
        double stretch = fmin(fabs(slideVel) * STRETCH_GAIN, STRETCH_MAX);
        double along   = fmin(1.0, base + stretch * (1.0 - base + 0.15));
        double across  = fmax(0.6, base - POP_SQUASH * over - stretch);
        BOOL   moveX   = fabs(inward.x) > fabs(inward.y);
        double sx = moveX ? along : across, sy = moveX ? across : along;
        NSAffineTransform *tf = [NSAffineTransform transform];
        [tf translateXBy:NSMidX(b) yBy:NSMidY(b)];
        [tf scaleXBy:sx yBy:sy];
        [tf translateXBy:-NSMidX(b) yBy:-NSMidY(b)];
        [tf concat];
    }

    // ---- body -----------------------------------------------------------
    NSBezierPath *body = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(b, 0.5, 0.5)
                                                         xRadius:RADIUS yRadius:RADIUS];
    [[NSColor colorWithCalibratedRed:0 green:0 blue:0 alpha:0.94] setFill];
    [body fill];
    [[NSColor colorWithCalibratedWhite:1.0 alpha:0.045] setStroke];
    [body setLineWidth:1.0];
    [body stroke];

    // The history panel draws its own scrollable list on top (as real
    // AppKit subviews, not here) — this view's only job while it is open is
    // the flat rounded backdrop just filled above.
    if (historyOpen) return;

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
        double desired = clamp01(level * 2.6) * env * wob;
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

    // ---- status label (Error only) -------------------------------------
    if (showLabel) {
        NSRect textArea = vert
            ? NSMakeRect(NSMaxX(core), 0, b.size.width - core.size.width, b.size.height)
            : NSMakeRect(0, 0, b.size.width, b.size.height - core.size.height);
        static NSMutableParagraphStyle *style = nil;
        if (!style) {
            style = [[NSMutableParagraphStyle alloc] init];
            style.alignment = NSTextAlignmentCenter;
            style.lineBreakMode = NSLineBreakByTruncatingTail;
        }
        NSDictionary *attrs = @{
            NSFontAttributeName: [NSFont systemFontOfSize:LABEL_FONT_SIZE],
            NSForegroundColorAttributeName: [NSColor colorWithCalibratedWhite:0.92 alpha:0.88 * barsA],
            NSParagraphStyleAttributeName: style,
        };
        NSSize sz = [label sizeWithAttributes:attrs];
        NSRect textRect = NSMakeRect(NSMinX(textArea) + 4,
                                      NSMidY(textArea) - sz.height / 2,
                                      fmax(0, textArea.size.width - 8),
                                      sz.height);
        [label drawInRect:textRect withAttributes:attrs];
    }
}
@end

// ---- panel -----------------------------------------------------------------

static NSPanel    *panel = nil;
static FLPillView *view  = nil;
static NSTimer    *timer = nil;
static double      shownAt = 0;   // fade-in start
static double      hideAt  = 0;   // >0 while fading out (may be in the future)
static double      slideLastT = 0;   // previous tick() time, for the springs' own dt
static NSPoint     baseOrigin;
// slidePos/slideVel/popPos/popVel/inward are defined near the top of the
// file so FLPillView can read them.

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
        // Normally ST_HIDDEN means "nothing to lose" and it's safe to drop
        // the window. While the history panel is open the view is forced to
        // ST_HIDDEN too (see flowlite_overlay_show_history), so that check
        // alone would rip the visible panel out from under the user on a
        // sleep/wake or display change — historyOpen is the exception.
        if (!view || (view->state == ST_HIDDEN && !view->historyOpen)) discardPanel();
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

// historyKeyAllowed is the only thing that ever lets the panel become key:
// YES from the moment the history panel opens until the instant a row is
// picked or the panel starts closing (see historyResignKey). Outside that
// window FLPanel refuses key status exactly like a plain borderless NSPanel.
static BOOL historyKeyAllowed = NO;

// FLPanel exists solely so the history panel's search field can take typing.
// A borderless window says NO to canBecomeKeyWindow by default; combined
// with NSWindowStyleMaskNonactivatingPanel (set in ensurePanel), saying YES
// lets the window receive keyboard input WITHOUT activating FlowLite — the
// user's app stays frontmost, which is what makes the paste land there.
@interface FLPanel : NSPanel
@end
@implementation FLPanel
- (BOOL)canBecomeKeyWindow { return historyKeyAllowed; }
@end

static void ensurePanel(void) {
    if (panel) return;
    NSRect frame = NSMakeRect(0, 0, PILL_LONG, PILL_SHORT);
    panel = [[FLPanel alloc] initWithContentRect:frame
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

// ---- label sizing -----------------------------------------------------

static BOOL showsLabel(void) {
    return view && view->label.length > 0 && view->state == ST_ERROR;
}

static NSDictionary *labelAttributes(void) {
    static NSMutableParagraphStyle *style = nil;
    if (!style) {
        style = [[NSMutableParagraphStyle alloc] init];
        style.alignment = NSTextAlignmentCenter;
        style.lineBreakMode = NSLineBreakByTruncatingTail;
    }
    return @{
        NSFontAttributeName: [NSFont systemFontOfSize:LABEL_FONT_SIZE],
        NSParagraphStyleAttributeName: style,
    };
}

// The width needed to lay the current label out on one line, clamped so a
// long sentence truncates instead of growing the pill without bound.
static double pillWidth(void) {
    double base = vertical() ? PILL_SHORT : PILL_LONG;
    if (!showsLabel()) return base;
    double want = ceil([view->label sizeWithAttributes:labelAttributes()].width) + 2 * LABEL_SIDE_PAD;
    return fmax(base, fmin(want, LABEL_MAX_TEXT + 2 * LABEL_SIDE_PAD));
}

// Standing upright, the label sits beside the bars within the same height;
// lying flat, it needs its own row below them.
static double pillHeight(void) {
    double base = vertical() ? PILL_LONG : PILL_SHORT;
    return (showsLabel() && !vertical()) ? base + LABEL_THICK : base;
}

// computeOrigin fills in baseOrigin/inward for a pill of size w×h on scr,
// without touching the panel's actual frame — split out of layout() so the
// history morph can ask "where would the plain pill sit right now" purely as
// numbers, to use as an animation endpoint, without snapping the panel there
// first.
static void computeOrigin(NSScreen *scr, double w, double h) {
    NSRect sf = [scr frame];
    switch (position) {
        case POS_TOP:
            baseOrigin = NSMakePoint(NSMidX(sf) - w / 2, NSMaxY(sf) - topInset(scr) - edgeGap() - h);
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
}

static void layout(NSScreen *scr, double w, double h) {
    computeOrigin(scr, w, h);
    [panel setFrame:NSMakeRect(baseOrigin.x, baseOrigin.y, w, h) display:NO];
}

// The screen the pill last chose to sit on, so a resize can keep it there
// without re-picking based on the mouse.
static NSScreen *targetScreen = nil;

// Centred on the chosen edge of whichever screen holds the mouse, EDGE_GAP
// in from the physical edge (not the visibleFrame — the Dock does not push
// it). Re-picks the screen from the current mouse location, so call this
// only when the pill is starting a fresh appearance, not on every state
// change.
static void reposition(void) {
    NSPoint mouse = [NSEvent mouseLocation];
    NSScreen *target = [NSScreen mainScreen];
    for (NSScreen *s in [NSScreen screens]) {
        if (NSMouseInRect(mouse, [s frame], NO)) { target = s; break; }
    }
    targetScreen = target;
    layout(target, pillWidth(), pillHeight());
}

// Re-applies the current label's size requirement in place, without picking
// a new screen — used on a state change to an already-visible pill, so a
// label appearing, growing, or clearing never makes the pill jump displays.
static void resizeInPlace(void) {
    if (!panel || !targetScreen) return;
    layout(targetScreen, pillWidth(), pillHeight());
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
    double slideTarget = 0;                              // resting position, onscreen
    double popTarget   = 1.0;
    if (hideAt > 0) {
        double f = easeInOut((t - hideAt) / FADE_OUT);
        alpha *= (1.0 - f);
        slideTarget = -SLIDE_TRAVEL;                     // springs back toward the edge
        popTarget   = POP_FROM;                          // and shrinks as it goes
        if (f >= 1.0) {
            [panel orderOut:nil];
            view->state = ST_HIDDEN;
            view->label = nil;
            hideAt = 0;
            checkAt = 0;
            stopTimer();
            return;
        }
    }

    // A small underdamped spring carries the slide offset toward
    // slideTarget instead of an eased curve, so the pill overshoots its
    // resting position by a point or two before settling — a soft pop
    // rather than a flat slide. See SLIDE_STIFFNESS/SLIDE_DAMPING above.
    double dt = slideLastT > 0 ? fmin(t - slideLastT, 0.1) : 1.0 / FPS;
    slideLastT = t;
    double accel = -SLIDE_STIFFNESS * (slidePos - slideTarget) - SLIDE_DAMPING * slideVel;
    slideVel += accel * dt;
    slidePos += slideVel * dt;
    double paccel = -POP_STIFFNESS * (popPos - popTarget) - POP_DAMPING * popVel;
    popVel += paccel * dt;
    popPos += popVel * dt;

    [panel setAlphaValue:alpha];
    [panel setFrameOrigin:NSMakePoint(baseOrigin.x + inward.x * slidePos, baseOrigin.y + inward.y * slidePos)];
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

// text is only ever kept for the terminal Error state — every other state
// draws no words, per the pill's design. text is a short-lived C
// string the Go side frees right after this call returns (see
// overlay_darwin.go), so it must be copied into Cocoa-owned storage
// synchronously, here, rather than held onto past this function's return.
static NSString *labelFor(int state, const char *text) {
    if (!text || !text[0]) return nil;
    if (state != ST_ERROR) return nil;
    return [NSString stringWithUTF8String:text];
}

void flowlite_overlay_show(int state, const char *text) {
    @autoreleasepool {
        ensurePanel();
        // The history panel owns the pill's window while it is open (see
        // flowlite_overlay_show_history); the daemon should not be trying to
        // show a dictation state then, but guard against a stray/stale call
        // stomping the panel's frame regardless.
        if (view->historyOpen) return;
        BOOL fresh = (view->state == ST_HIDDEN) || hideAt > 0;
        double t = now_s();
        if (fresh && terminal(state)) {
            // Nothing to show: success/cancel have no look of their own.
            if (view->state != ST_HIDDEN) flowlite_overlay_hide();
            return;
        }
        view->state = state;
        view->stateAt = t;
        view->label = labelFor(state, text);
        hideAt = 0;
        if (fresh) {
            shownAt = t;
            view->appearAt = t;
            view->lastTick = 0;
            view->level = view->target = 0;
            view->collapse = (state == ST_LISTENING) ? 0 : 1;
            view->red = view->shimmer = 0;
            for (int i = 0; i < BARS; i++) view->bars[i] = 0;
            // Seed the slide spring fresh so a rapid re-show never carries
            // stale velocity into a new appearance: starts off the edge,
            // at rest, same as the old flat ease's initial position.
            slidePos = -SLIDE_TRAVEL;
            slideVel = 0;
            popPos = POP_FROM;
            popVel = 0;
            slideLastT = 0;
            reposition();
            applyTraits(panel);
            [panel setAlphaValue:0];
            [panel orderFrontRegardless];
            checkAt = t + 0.12;
        } else {
            resizeInPlace();
        }
        startTimer();
        [view setNeedsDisplay:YES];
    }
}

void flowlite_overlay_set_state(int state, const char *text) {
    @autoreleasepool {
        if (view && view->historyOpen) return; // see flowlite_overlay_show
        if (!panel || view->state == ST_HIDDEN) { flowlite_overlay_show(state, text); return; }
        view->state = state;
        view->stateAt = now_s();
        view->label = labelFor(state, text);
        hideAt = 0;
        if (terminal(state)) hideAt = view->stateAt;    // fade out right away
        resizeInPlace();
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
        if (view && view->historyOpen) return; // see flowlite_overlay_show
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

// ---- history panel ----------------------------------------------------
//
// The pill morphs into a plain NSScrollView/NSTableView holding one row per
// remembered transcript. This is a deliberate, scoped departure from the
// rest of the file in two ways, both called out again where they happen:
//   1. the panel briefly stops ignoring mouse events (a real, if narrow,
//      exception to "must never take focus" — reverted the instant it
//      closes), because a table you cannot click is useless;
//   2. the morph itself is driven by NSAnimationContext + NSAnimator, not
//      the hand-rolled tick() timer — a real interactive AppKit control
//      needs the standard event/animation machinery, not a manual one.

static NSArray       *historyAllRows = nil;  // every {index, time, preview} the Go side passed
static NSArray       *historyRows    = nil;  // the VISIBLE subset after the search filter, same order
static NSScrollView  *historyScroll  = nil;
static NSTableView   *historyTable   = nil;
static NSTextField   *historySearch  = nil;  // the single-line filter field above the table
static NSArray<NSLayoutConstraint *> *historyConstraints = nil; // active only while the panel is open
static NSView        *historySep     = nil;  // hairline between the field and the table
static NSTextField   *historyEmpty   = nil;  // "No matches", shown only when the filter hides every row
static id             historyDS      = nil;  // FLHistoryDataSource instance
static id             historyEscMonitor   = nil;
static id             historyClickMonitor = nil;

#define HIST_SEARCH_H   22.0   // height of the search field
#define HIST_SEARCH_GAP  6.0   // gap field -> separator, and separator -> table

// Forward declarations: FLHistoryDataSource and installHistoryMonitors below
// both close the panel by calling flowlite_overlay_hide_history, ahead of its
// own definition further down this section (mirrors the existing forward
// declaration of flowlite_overlay_hide near terminal()/flowlite_overlay_show
// above). The other three are the search field's shared key handling.
void flowlite_overlay_hide_history(void);
static void historyApplyFilter(void);
static void historyPickRow(NSInteger row);
static void historyMoveSelection(NSInteger delta);

@interface FLHistoryDataSource : NSObject <NSTableViewDataSource, NSTableViewDelegate, NSTextFieldDelegate>
@end

@implementation FLHistoryDataSource
- (NSInteger)numberOfRowsInTableView:(NSTableView *)tv {
    (void)tv;
    return (NSInteger)historyRows.count;
}
// ---- search field delegate ----
// Live filter: every edit re-derives the visible rows from the full list.
- (void)controlTextDidChange:(NSNotification *)note {
    (void)note;
    historyApplyFilter();
}
// The field keeps first responder for the whole life of the panel — typing
// always lands in it — and the navigation keys are intercepted here, at the
// field editor, instead of being let through to the text view's own
// (useless, single-line) handling of them.
- (BOOL)control:(NSControl *)control textView:(NSTextView *)tv doCommandBySelector:(SEL)sel {
    (void)control; (void)tv;
    if (sel == @selector(moveUp:))   { historyMoveSelection(-1); return YES; }
    if (sel == @selector(moveDown:)) { historyMoveSelection(+1); return YES; }
    if (sel == @selector(insertNewline:)) {
        // Enter acts on the highlighted row if the user arrowed to one,
        // otherwise on the first visible row — the same code path a click
        // takes (historyPickRow), so it also closes the panel.
        NSInteger row = historyTable.selectedRow;
        historyPickRow(row >= 0 ? row : 0);
        return YES;
    }
    if (sel == @selector(cancelOperation:)) {
        // Escape: clear a non-empty query first; a second press closes.
        if (historySearch.stringValue.length > 0) {
            historySearch.stringValue = @"";
            historyApplyFilter();
        } else {
            flowlite_overlay_hide_history();
        }
        return YES;
    }
    return NO;
}
// Each row is a plain NSView "cell" holding two subviews, pinned to the
// cell's edges with Auto Layout (not a hand-set frame): the table itself sets
// the cell's frame per the column's geometry, and — because
// usesAutomaticRowHeights is on (see ensureHistoryUI) — asks Auto Layout for
// the height that fits the (now narrower) preview field's wrapped text at
// that width, per row. That is why the preview field's width must come from
// the column (col.width, fixed once in ensureHistoryUI) rather than
// tv.bounds.size.width: the latter is only whatever the table view's frame
// happens to be at the moment a row is requested, which can still be
// zero/stale this early, whereas the column's width is authoritative and
// already settled.
//
// The two subviews are a fixed-width, non-wrapping time label ("01:26") and a
// separate wrapping preview field, rather than one field holding the
// concatenated "<time>   <preview>" string. A single concatenated string
// wraps as one paragraph, so any continuation line falls back to the field's
// own left edge — the same horizontal position as the time prefix — instead
// of staying indented under where the transcript text actually starts. With
// two independently-constrained fields, every wrapped line of the preview
// (including continuation lines) is pinned to the same leading edge, just
// past the time label — the standard hanging-indent list-row look.
- (NSView *)tableView:(NSTableView *)tv viewForTableColumn:(NSTableColumn *)col row:(NSInteger)row {
    (void)tv;
    NSView *cell = [[NSView alloc] initWithFrame:NSZeroRect];
    cell.translatesAutoresizingMaskIntoConstraints = NO;

    NSDictionary *r = historyRows[(NSUInteger)row];

    NSTextField *timeLabel = [[NSTextField alloc] initWithFrame:NSZeroRect];
    timeLabel.editable = NO;
    timeLabel.bordered = NO;
    timeLabel.drawsBackground = NO;
    timeLabel.selectable = NO;
    timeLabel.font = [NSFont systemFontOfSize:11.0];
    // A touch dimmer than the preview text so it reads as a timestamp, not
    // content — same near-white/dim-white palette as the rest of this UI.
    timeLabel.textColor = [NSColor colorWithCalibratedWhite:0.92 alpha:0.55];
    timeLabel.lineBreakMode = NSLineBreakByClipping; // fixed-width and short; never wraps
    timeLabel.translatesAutoresizingMaskIntoConstraints = NO;
    timeLabel.stringValue = r[@"time"];

    NSTextField *tf = [[NSTextField alloc] initWithFrame:NSZeroRect];
    tf.editable = NO;
    tf.bordered = NO;
    tf.drawsBackground = NO;
    tf.selectable = NO;
    tf.font = [NSFont systemFontOfSize:12.0];
    tf.textColor = [NSColor colorWithCalibratedWhite:0.92 alpha:0.92];
    tf.cell.wraps = YES;
    tf.lineBreakMode = NSLineBreakByWordWrapping;
    tf.maximumNumberOfLines = HIST_ROW_MAXLINES;
    tf.cell.truncatesLastVisibleLine = YES; // end the 3-line cap with an ellipsis, not a bare cut
    // The preview no longer shares its line with the time label, so its wrap
    // width is the column width minus the time label's fixed width and gap —
    // not the full column width as before.
    tf.preferredMaxLayoutWidth = col.width - HIST_TIME_W - HIST_TIME_GAP;
    tf.translatesAutoresizingMaskIntoConstraints = NO;
    tf.stringValue = r[@"preview"];

    [cell addSubview:timeLabel];
    [cell addSubview:tf];
    [NSLayoutConstraint activateConstraints:@[
        [timeLabel.leadingAnchor constraintEqualToAnchor:cell.leadingAnchor],
        [timeLabel.topAnchor     constraintEqualToAnchor:cell.topAnchor constant:HIST_ROW_VPAD],
        [timeLabel.widthAnchor   constraintEqualToConstant:HIST_TIME_W],

        [tf.leadingAnchor  constraintEqualToAnchor:timeLabel.trailingAnchor constant:HIST_TIME_GAP],
        [tf.trailingAnchor constraintEqualToAnchor:cell.trailingAnchor],
        // Same top pin as the time label, so both start on the row's first
        // line — not vertically centred against the preview's full (possibly
        // multi-line) height, which would float the time label away from the
        // first line once text wraps to 2-3 lines.
        [tf.topAnchor      constraintEqualToAnchor:cell.topAnchor    constant:HIST_ROW_VPAD],
        // Still the height-driving pin: the row's automatic height comes from
        // the preview field's (now narrower) wrapped intrinsic size.
        [tf.bottomAnchor   constraintEqualToAnchor:cell.bottomAnchor constant:-HIST_ROW_VPAD],
    ]];
    return cell;
}
// A click on a row pastes it. This is the table's target/action (set in
// ensureHistoryUI), NOT tableViewSelectionDidChange: Up/Down from the search
// field now move the selection as a highlight, and selection-triggered
// picking would paste on the first arrow press.
- (void)rowClicked:(id)sender {
    (void)sender;
    historyPickRow(historyTable.clickedRow);
}
@end

// historyResignKey drops the panel's keyboard focus synchronously, BEFORE the
// Go side is told about a pick. inject.Paste posts Cmd+V through
// CGEventPost(kCGHIDEventTap), which the window server routes to whichever
// process currently holds key focus — while the search field has it, that is
// FlowLite, and the transcript would be pasted into our own field instead of
// the user's app. Ordering the (non-activating) panel out is what hands key
// focus back to the previously-key window of the active app; it is ordered
// straight back in, un-keyed, so the shrink animation still has something to
// draw. Both calls land in the same window-server transaction, so nothing
// visibly blinks. After this canBecomeKeyWindow says NO again.
static void historyResignKey(void) {
    historyKeyAllowed = NO;
    if (!panel) return;
    [panel makeFirstResponder:nil];
    if ([panel isKeyWindow] || [panel isVisible]) {
        [panel orderOut:nil];
        [panel orderFrontRegardless];
    }
}

// historyPickRow maps a VISIBLE row back to the ORIGINAL index the Go side
// passed (the "index" each row carries — never the table row number, which
// changes with the filter), then dismisses the panel. Key focus is given up
// first (see historyResignKey) so the paste the Go callback triggers lands in
// the user's app, however quickly its goroutine gets to the keystroke.
static void historyPickRow(NSInteger row) {
    if (row < 0 || (NSUInteger)row >= historyRows.count) return;
    NSDictionary *r = historyRows[(NSUInteger)row];
    int idx = [r[@"index"] intValue];
    historyResignKey();
    flowliteHistoryPick(idx);
    flowlite_overlay_hide_history(); // picking a row both acts on it and dismisses the panel
}

// historyMoveSelection moves the highlight by delta (clamped) and scrolls it
// into view; the row is only acted on by Enter or a click.
static void historyMoveSelection(NSInteger delta) {
    NSInteger n = (NSInteger)historyRows.count;
    if (n == 0) return;
    NSInteger cur = historyTable.selectedRow;
    NSInteger next = cur < 0 ? (delta > 0 ? 0 : n - 1) : cur + delta;
    if (next < 0) next = 0;
    if (next >= n) next = n - 1;
    [historyTable selectRowIndexes:[NSIndexSet indexSetWithIndex:(NSUInteger)next] byExtendingSelection:NO];
    [historyTable scrollRowToVisible:next];
}

// historyApplyFilter derives historyRows from historyAllRows: a
// case-insensitive substring match on the preview text, original order kept,
// everything when the query is empty. It swaps the table for the "No matches"
// line when nothing survives and drops the selection, which pointed at rows
// of the previous filter.
static void historyApplyFilter(void) {
    NSString *q = [historySearch.stringValue stringByTrimmingCharactersInSet:[NSCharacterSet whitespaceCharacterSet]];
    if (q.length == 0) {
        historyRows = historyAllRows ?: @[];
    } else {
        NSMutableArray *keep = [NSMutableArray array];
        for (NSDictionary *r in historyAllRows) {
            NSString *p = r[@"preview"];
            if ([p isKindOfClass:[NSString class]] &&
                [p rangeOfString:q options:NSCaseInsensitiveSearch].location != NSNotFound) {
                [keep addObject:r];
            }
        }
        historyRows = keep;
    }
    [historyTable deselectAll:nil];
    [historyTable reloadData];
    BOOL none = historyRows.count == 0 && historyAllRows.count > 0;
    historyEmpty.hidden = !none;
    historyTable.hidden = none;
}

// ensureHistoryUI lazily builds the scroll view/table once and keeps it as a
// subview of the pill's own view — it persists across panel rebuilds the
// same way the view itself does (see discardPanel/ensurePanel).
static void ensureHistoryUI(void) {
    if (historyScroll) return;
    historyDS = [[FLHistoryDataSource alloc] init];

    historyTable = [[NSTableView alloc] initWithFrame:NSZeroRect];
    // Nothing else keeps the table's own outer width in sync with the scroll
    // view's clip width as the panel resizes — without this, the table can
    // drift wider than the visible area and the scroll view lets you pan
    // horizontally even though columnAutoresizingStyle below keeps the
    // *column* filling whatever width the table currently has. This is the
    // standard idiom for a frame-based NSTableView inside a legacy-frame
    // NSScrollView: it makes NSScrollView's internal tiling resize the table
    // to track the clip view's width on every layout pass.
    historyTable.autoresizingMask = NSViewWidthSizable;
    // macOS 11+ defaults to the "inset" table style, which pads every cell
    // 16pt on each side and makes the table 32pt WIDER than its clip view —
    // so cells started at x=16, the column's 324pt ran 16pt past the visible
    // edge, and text wrapped at the column width was hard-clipped mid-word.
    // Plain style is what the geometry below (col.width == clip width,
    // HIST_PAD_SIDE as the only side padding) assumes.
    if (@available(macOS 11.0, *)) historyTable.style = NSTableViewStylePlain;
    NSTableColumn *col = [[NSTableColumn alloc] initWithIdentifier:@"row"];
    col.width = HIST_W - 2 * HIST_PAD_SIDE;
    [historyTable addTableColumn:col];
    historyTable.headerView = nil;
    historyTable.backgroundColor = [NSColor clearColor];
    // HIST_ROW_H remains as the estimate NSTableView uses for rows it hasn't
    // measured yet (e.g. while fast-scrolling); usesAutomaticRowHeights makes
    // it ask Auto Layout for each row's real, possibly-multi-line height
    // instead of enforcing this as a hard cap — the fix for wrapped preview
    // text getting clipped by a fixed row height.
    historyTable.rowHeight = HIST_ROW_H;
    historyTable.usesAutomaticRowHeights = YES;
    historyTable.intercellSpacing = NSMakeSize(0, 0);
    historyTable.columnAutoresizingStyle = NSTableViewUniformColumnAutoresizingStyle;
    historyTable.dataSource = historyDS;
    historyTable.delegate = historyDS;
    // Click-to-paste goes through the action, not the selection (see
    // FLHistoryDataSource's rowClicked:).
    historyTable.target = historyDS;
    historyTable.action = @selector(rowClicked:);
    // The table never takes typing itself: a click on a row must not steal
    // first responder from the search field, or Up/Down/Enter would start
    // going to the table and the user's next keystroke would vanish.
    historyTable.refusesFirstResponder = YES;

    // The search field: dark, borderless, one line, sitting inside the same
    // HIST_PAD_SIDE gutter as the table so its text lines up with the rows.
    historySearch = [[NSTextField alloc] initWithFrame:NSZeroRect];
    historySearch.editable = YES;
    historySearch.selectable = YES;
    historySearch.bordered = NO;
    historySearch.bezeled = NO;
    historySearch.drawsBackground = NO;
    historySearch.focusRingType = NSFocusRingTypeNone;
    historySearch.font = [NSFont systemFontOfSize:12.0];
    historySearch.textColor = [NSColor colorWithCalibratedWhite:0.92 alpha:0.92];
    historySearch.usesSingleLineMode = YES;
    historySearch.lineBreakMode = NSLineBreakByClipping;
    historySearch.cell.scrollable = YES;
    historySearch.placeholderAttributedString = [[NSAttributedString alloc]
        initWithString:@"Search"
            attributes:@{ NSFontAttributeName: [NSFont systemFontOfSize:12.0],
                          NSForegroundColorAttributeName: [NSColor colorWithCalibratedWhite:0.92 alpha:0.40] }];
    historySearch.delegate = historyDS;
    historySearch.translatesAutoresizingMaskIntoConstraints = NO;
    historySearch.hidden = YES;
    historySearch.alphaValue = 0;

    // A hairline, not an NSBox separator: NSBox picks a system separator
    // colour meant for light/dark chrome, which reads wrong on this black.
    historySep = [[NSView alloc] initWithFrame:NSZeroRect];
    historySep.wantsLayer = YES;
    historySep.layer.backgroundColor = [NSColor colorWithCalibratedWhite:1.0 alpha:0.14].CGColor;
    historySep.translatesAutoresizingMaskIntoConstraints = NO;
    historySep.hidden = YES;
    historySep.alphaValue = 0;

    historyEmpty = [[NSTextField alloc] initWithFrame:NSZeroRect];
    historyEmpty.editable = NO;
    historyEmpty.bordered = NO;
    historyEmpty.drawsBackground = NO;
    historyEmpty.selectable = NO;
    historyEmpty.alignment = NSTextAlignmentCenter;
    historyEmpty.font = [NSFont systemFontOfSize:12.0];
    historyEmpty.textColor = [NSColor colorWithCalibratedWhite:0.92 alpha:0.45];
    historyEmpty.stringValue = @"No matches";
    historyEmpty.translatesAutoresizingMaskIntoConstraints = NO;
    historyEmpty.hidden = YES;

    historyScroll = [[NSScrollView alloc] initWithFrame:NSZeroRect];
    historyScroll.documentView = historyTable;
    historyScroll.hasVerticalScroller = YES;
    // Belt-and-braces: the width-tracking above should make an actual
    // horizontal scroller unnecessary, but disable it explicitly so none can
    // ever appear (and so the intent — vertical-only scrolling, fixed width,
    // no horizontal overflow — is unambiguous to the next reader).
    historyScroll.hasHorizontalScroller = NO;
    // hasHorizontalScroller only controls whether a scroller is drawn; it
    // does not stop a trackpad swipe with a horizontal component from
    // producing a visible rubber-band bounce, which NSScrollView allows by
    // default (horizontalScrollElasticity defaults to
    // NSScrollElasticityAutomatic) independent of whether there's anything
    // real to scroll to. Lock that down explicitly; vertical elasticity is
    // left at its default so vertical scroll/bounce still works normally.
    historyScroll.horizontalScrollElasticity = NSScrollElasticityNone;
    historyScroll.drawsBackground = NO;
    historyScroll.hidden = YES;
    historyScroll.alphaValue = 0;
    // Auto Layout, not a hand-set frame: this view's frame used to be set
    // absolutely in flowlite_overlay_show_history using the panel's FINAL
    // target size, at a moment when `view` (its superview, whose bounds that
    // frame is relative to) was still sized like the small plain pill —
    // combined with a spring autoresizingMask, that mismatch got baked in as
    // fixed margins and produced a badly oversized/mispositioned scroll view
    // for the rest of the panel's life (see the history row cells just above,
    // which already avoid exactly this with constraints instead of a frame).
    historyScroll.translatesAutoresizingMaskIntoConstraints = NO;
    [view addSubview:historySearch];
    [view addSubview:historySep];
    [view addSubview:historyScroll];
    [view addSubview:historyEmpty];
    // Built once, but only ACTIVE while the panel is open (see
    // flowlite_overlay_show_history / closeHistory). A hidden view still
    // takes part in Auto Layout, so leaving these on would pin a minimum
    // height of HIST_PAD_TOP + HIST_SEARCH_H + 2*HIST_SEARCH_GAP + 1 +
    // HIST_PAD_BOTTOM (= 55pt) on `view` for good — and AppKit then refuses
    // to size the window's content view any smaller, so the plain
    // PILL_LONG x PILL_SHORT capsule came back as a 100x55 rounded box after
    // the history panel had been opened once.
    historyConstraints = @[
        // Search field across the top, inside the side gutter.
        [historySearch.leadingAnchor  constraintEqualToAnchor:view.leadingAnchor  constant:HIST_PAD_SIDE],
        [historySearch.trailingAnchor constraintEqualToAnchor:view.trailingAnchor constant:-HIST_PAD_SIDE],
        [historySearch.topAnchor      constraintEqualToAnchor:view.topAnchor      constant:HIST_PAD_TOP],
        [historySearch.heightAnchor   constraintEqualToConstant:HIST_SEARCH_H],
        // Hairline under it.
        [historySep.leadingAnchor  constraintEqualToAnchor:view.leadingAnchor  constant:HIST_PAD_SIDE],
        [historySep.trailingAnchor constraintEqualToAnchor:view.trailingAnchor constant:-HIST_PAD_SIDE],
        [historySep.topAnchor      constraintEqualToAnchor:historySearch.bottomAnchor constant:HIST_SEARCH_GAP],
        [historySep.heightAnchor   constraintEqualToConstant:1.0],
        // The table takes the rest.
        [historyScroll.leadingAnchor  constraintEqualToAnchor:view.leadingAnchor  constant:HIST_PAD_SIDE],
        [historyScroll.trailingAnchor constraintEqualToAnchor:view.trailingAnchor constant:-HIST_PAD_SIDE],
        [historyScroll.topAnchor      constraintEqualToAnchor:historySep.bottomAnchor constant:HIST_SEARCH_GAP],
        [historyScroll.bottomAnchor   constraintEqualToAnchor:view.bottomAnchor   constant:-HIST_PAD_BOTTOM],
        // "No matches" sits centred where the table would be.
        [historyEmpty.leadingAnchor  constraintEqualToAnchor:historyScroll.leadingAnchor],
        [historyEmpty.trailingAnchor constraintEqualToAnchor:historyScroll.trailingAnchor],
        [historyEmpty.centerYAnchor  constraintEqualToAnchor:historyScroll.centerYAnchor],
    ];
}

// historyTargetFrame anchors the grown panel so it reads as the same object
// as the pill, expanding away from whichever screen edge the pill sits on
// (the edge the pill's own inward/position logic already tracks), then
// clamps it fully on-screen in case the pill sat near a corner.
static NSRect historyTargetFrame(NSRect start, double w, double h) {
    double x, y;
    switch (position) {
        case POS_TOP:
            x = NSMidX(start) - w / 2;
            y = NSMaxY(start) - h;       // top edge stays put, grows downward
            break;
        case POS_LEFT:
            x = NSMinX(start);           // left edge stays put, grows rightward
            y = NSMidY(start) - h / 2;
            break;
        case POS_RIGHT:
            x = NSMaxX(start) - w;       // right edge stays put, grows leftward
            y = NSMidY(start) - h / 2;
            break;
        default: // POS_BOTTOM
            x = NSMidX(start) - w / 2;
            y = NSMinY(start);           // bottom edge stays put, grows upward
            break;
    }
    NSRect vis = [(targetScreen ?: [NSScreen mainScreen]) visibleFrame];
    if (x < NSMinX(vis)) x = NSMinX(vis);
    if (x + w > NSMaxX(vis)) x = NSMaxX(vis) - w;
    if (y < NSMinY(vis)) y = NSMinY(vis);
    if (y + h > NSMaxY(vis)) y = NSMaxY(vis) - h;
    return NSMakeRect(x, y, w, h);
}

static void installHistoryMonitors(void) {
    if (historyEscMonitor) return;
    historyEscMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown
        handler:^NSEvent *(NSEvent *e) {
            if (!view->historyOpen) return e;
            // Normal path: the panel is key and the search field is first
            // responder, so the field editor gets the event and
            // FLHistoryDataSource's doCommandBySelector: handles
            // Escape/Enter/Up/Down. Let it through untouched.
            if (panel && [panel isKeyWindow] && [panel firstResponder] != nil &&
                [panel firstResponder] != (NSResponder *)panel) {
                return e;
            }
            // Fallback: the event reached this process but the panel is not
            // key (e.g. key status was refused). Route it into the field by
            // hand so the panel is still fully usable from the keyboard.
            switch (e.keyCode) {
                case 53: // Escape, see hotkey/tap_darwin.go's escapeKeycode
                    if (historySearch.stringValue.length > 0) { historySearch.stringValue = @""; historyApplyFilter(); }
                    else flowlite_overlay_hide_history();
                    return nil;
                case 36: case 76: // Return, keypad Enter
                    historyPickRow(historyTable.selectedRow >= 0 ? historyTable.selectedRow : 0);
                    return nil;
                case 126: historyMoveSelection(-1); return nil; // Up
                case 125: historyMoveSelection(+1); return nil; // Down
                case 51: { // Delete
                    NSString *s = historySearch.stringValue;
                    if (s.length > 0) {
                        historySearch.stringValue = [s substringToIndex:s.length - 1];
                        historyApplyFilter();
                    }
                    return nil;
                }
                default: break;
            }
            if (e.modifierFlags & (NSEventModifierFlagCommand | NSEventModifierFlagControl)) return e;
            NSString *chars = e.characters;
            if (chars.length > 0 && [chars characterAtIndex:0] >= 0x20) {
                historySearch.stringValue = [historySearch.stringValue stringByAppendingString:chars];
                historyApplyFilter();
                return nil;
            }
            return e;
        }];
    // A global monitor only ever sees events delivered to OTHER applications
    // (Apple's documented behaviour for addGlobalMonitorForEventsMatchingMask),
    // so a click on our own table row never reaches this handler — only a
    // click that lands outside FlowLite entirely does, which is exactly the
    // "dismiss on outside click" behaviour NSPopover itself relies on this
    // same mechanism for.
    historyClickMonitor = [NSEvent addGlobalMonitorForEventsMatchingMask:(NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown)
        handler:^(NSEvent *e) {
            (void)e;
            if (!view->historyOpen || !panel) return;
            NSPoint loc = [NSEvent mouseLocation];
            if (!NSPointInRect(loc, [panel frame])) flowlite_overlay_hide_history();
        }];
}

static void removeHistoryMonitors(void) {
    if (historyEscMonitor) { [NSEvent removeMonitor:historyEscMonitor]; historyEscMonitor = nil; }
    if (historyClickMonitor) { [NSEvent removeMonitor:historyClickMonitor]; historyClickMonitor = nil; }
}

void flowlite_overlay_show_history(const char *entriesJSON) {
    @autoreleasepool {
        ensurePanel();
        ensureHistoryUI();

        NSData *data = [NSData dataWithBytes:entriesJSON length:strlen(entriesJSON)];
        NSError *jerr = nil;
        NSArray *rows = [NSJSONSerialization JSONObjectWithData:data options:0 error:&jerr];
        historyAllRows = [rows isKindOfClass:[NSArray class]] ? rows : @[];
        if (!view->historyOpen) historySearch.stringValue = @""; // a fresh open always starts unfiltered
        historyApplyFilter(); // (re)derives historyRows and reloads the table

        if (view->historyOpen) return; // already open: the list above is all that changes

        // Where the pill currently sits (or would sit, freshly placed on
        // whichever screen the mouse is on, the same way a fresh Show does)
        // is the animation's starting frame.
        BOOL pillVisible = panel && view->state != ST_HIDDEN && [panel alphaValue] > 0.01;
        NSRect startFrame;
        if (pillVisible) {
            startFrame = [panel frame];
            if (!targetScreen) targetScreen = [NSScreen mainScreen];
        } else {
            reposition(); // sets baseOrigin/inward/targetScreen and lays the panel out at pill size
            startFrame = [panel frame];
        }

        view->historyOpen = YES;
        view->state = ST_HIDDEN; // suppress bars/label; the table draws everything else now
        stopTimer();
        hideAt = 0;
        checkAt = 0;

        NSRect vis = [(targetScreen ?: [NSScreen mainScreen]) visibleFrame];
        double w = fmin(HIST_W, vis.size.width - 2 * edgeGap());
        double h = fmin(HIST_H, vis.size.height - 2 * edgeGap());
        NSRect target = historyTargetFrame(startFrame, w, h);

        // historyScroll's size/position come entirely from the Auto Layout
        // constraints built once in ensureHistoryUI, which track `view`'s
        // own bounds as the panel resizes (see the animation below). They
        // are switched on only now, and off again in closeHistory, so the
        // plain pill is never held to the panel's minimum height.
        [NSLayoutConstraint activateConstraints:historyConstraints];
        historyScroll.alphaValue = 0;
        historyScroll.hidden = NO;
        historySearch.alphaValue = 0;
        historySearch.hidden = NO;
        historySep.alphaValue = 0;
        historySep.hidden = NO;

        // The ONE place in this file ignoresMouseEvents becomes NO: a table
        // you cannot click is pointless. Reverted in closeHistory below the
        // instant the panel finishes shrinking back down.
        [panel setIgnoresMouseEvents:NO];
        [panel setAlphaValue:1.0];
        [panel setFrame:startFrame display:YES];
        // ...and the ONE place it becomes key: typing has to land in the
        // search field. FLPanel + NSWindowStyleMaskNonactivatingPanel make
        // this possible without activating FlowLite, so the user's app stays
        // frontmost the whole time. Given back in historyResignKey.
        historyKeyAllowed = YES;
        [panel makeKeyAndOrderFront:nil];
        [panel makeFirstResponder:historySearch];
        // The field editor is a shared NSTextView whose caret defaults to
        // black — invisible on this surface.
        NSTextView *fe = (NSTextView *)[panel fieldEditor:YES forObject:historySearch];
        if ([fe isKindOfClass:[NSTextView class]]) {
            fe.insertionPointColor = [NSColor colorWithCalibratedWhite:0.92 alpha:0.92];
        }

        installHistoryMonitors();

        // A deliberate, explicit departure from this file's manual-NSTimer
        // animation style: a real interactive AppKit control (NSTableView)
        // wants the standard Cocoa animation/event machinery, not a
        // hand-rolled one.
        [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
            [ctx setDuration:HIST_ANIM_DUR];
            [ctx setTimingFunction:[CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut]];
            [[panel animator] setFrame:target display:YES];
            [[historyScroll animator] setAlphaValue:1.0];
            [[historySearch animator] setAlphaValue:1.0];
            [[historySep animator] setAlphaValue:1.0];
        } completionHandler:^{
            // nothing further to do — the panel is already interactive
        }];
    }
}

static void closeHistory(void) {
    if (!view || !view->historyOpen || !panel) return;
    removeHistoryMonitors();
    // Give keyboard focus back to the user's app right away (a no-op if a
    // pick already did it) — never after the animation, or an Escape-close
    // would leave their typing going nowhere for a quarter second.
    historyResignKey();

    NSScreen *scr = targetScreen ?: [NSScreen mainScreen];
    computeOrigin(scr, pillWidth(), pillHeight());
    NSRect pillFrame = NSMakeRect(baseOrigin.x, baseOrigin.y, pillWidth(), pillHeight());

    // Release the layout BEFORE the shrink starts: while active it holds the
    // content view at >= 55pt tall, and the window would stop short of the
    // pill's 30pt. The subviews keep their last frames (they are fading out
    // anyway) and are simply clipped as the window closes over them.
    [NSLayoutConstraint deactivateConstraints:historyConstraints];

    [NSAnimationContext runAnimationGroup:^(NSAnimationContext *ctx) {
        [ctx setDuration:HIST_ANIM_DUR];
        [ctx setTimingFunction:[CAMediaTimingFunction functionWithName:kCAMediaTimingFunctionEaseInEaseOut]];
        [[panel animator] setFrame:pillFrame display:YES];
        [[historyScroll animator] setAlphaValue:0.0];
        [[historySearch animator] setAlphaValue:0.0];
        [[historySep animator] setAlphaValue:0.0];
    } completionHandler:^{
        historyScroll.hidden = YES;
        historySearch.hidden = YES;
        historySep.hidden = YES;
        historyEmpty.hidden = YES;
        historyTable.hidden = NO;
        [panel setIgnoresMouseEvents:YES]; // restore the "never takes focus" invariant
        [panel setAlphaValue:0];
        [panel orderOut:nil];
        view->historyOpen = NO;
        view->state = ST_HIDDEN;
        historyAllRows = @[];
        historyRows = @[];
        historySearch.stringValue = @"";
        [historyTable reloadData];
        flowliteHistoryClosed();
    }];
}

void flowlite_overlay_hide_history(void) {
    @autoreleasepool { closeHistory(); }
}

// flowlite_overlay_history_set_query replaces the search field's text and
// re-filters, exactly as if the user had typed it. Exists for
// --history-preview-query so a filtered panel can be snapshotted offline.
void flowlite_overlay_history_set_query(const char *query) {
    @autoreleasepool {
        if (!historySearch) return;
        historySearch.stringValue = [NSString stringWithUTF8String:query ?: ""];
        // setStringValue on a field being edited selects the whole text;
        // a user who typed it would have the caret at the end instead.
        NSText *fe = [historySearch currentEditor];
        if (fe) fe.selectedRange = NSMakeRange(historySearch.stringValue.length, 0);
        historyApplyFilter();
    }
}

// flowlite_overlay_history_has_key reports whether the panel currently holds
// keyboard focus — what decides whether typing reaches the search field
// directly or only via the local-monitor fallback. Diagnostic, for the
// preview.
bool flowlite_overlay_history_has_key(void) {
    return panel != nil && [panel isKeyWindow];
}

bool flowlite_overlay_history_open(void) {
    return view != NULL && view->historyOpen;
}

// flowlite_overlay_snapshot_window renders the panel's content view — at
// whatever size it currently has, subviews included — so the grown history
// panel can be inspected the same way flowlite_overlay_snapshot inspects the
// pill. Layout is flushed first so pending Auto Layout passes land in the
// image rather than in the next frame.
bool flowlite_overlay_snapshot_window(const char *path) {
    @autoreleasepool {
        ensurePanel();
        NSView *cv = [panel contentView];
        [cv layoutSubtreeIfNeeded];
        [cv displayIfNeeded];
        NSBitmapImageRep *rep = [cv bitmapImageRepForCachingDisplayInRect:[cv bounds]];
        if (!rep) return false;
        [cv cacheDisplayInRect:[cv bounds] toBitmapImageRep:rep];
        NSData *png = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
        return [png writeToFile:[NSString stringWithUTF8String:path] atomically:YES];
    }
}
