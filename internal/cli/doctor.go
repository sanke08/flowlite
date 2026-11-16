package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/inject"
	"github.com/sanke08/flowlite/internal/permissions"
	"github.com/sanke08/flowlite/internal/whisper"
)

var (
	doctorRequest bool
	doctorDeep    bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check everything FlowLite needs and say exactly how to fix what is missing",
	Args:  cobra.NoArgs,
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	failed := 0
	// One spinner at a time: `check` starts it on the line the result will
	// occupy, and pass/fail clear it before printing so the line is replaced
	// in place. Stop is idempotent, so the deferred call only matters if a
	// check panics or returns early.
	var spin *spinner
	defer func() { spin.Stop() }()
	check := func(label, what string) {
		spin.Stop()
		spin = startSpinner(fmt.Sprintf("%-14s %s", label, what))
	}
	pass := func(label, detail string) {
		spin.Stop()
		fmt.Printf("  %s %-14s %s\n", ok("✓"), label, dim(detail))
	}
	fail := func(label, detail string) {
		spin.Stop()
		failed++
		fmt.Printf("  %s %-14s %s\n", bad("✗"), label, detail)
	}
	info := func(label, detail string) {
		fmt.Printf("    %-14s %s\n", label, detail)
	}

	fmt.Println(bold("FlowLite doctor"))
	fmt.Println()

	// -- identity: instant, before anything slow ------------------------------
	info("version", fmt.Sprintf("%s %s · whisper.cpp %s · %s %s/%s",
		Version, dim("("+Commit+", "+BuildDate+")"), WhisperVersion, runtime.Version(), runtime.GOOS, runtime.GOARCH))
	info("update", updateStatus())
	info("daemon", daemonStatus())

	// -- speech engine --------------------------------------------------
	check("engine", "loading whisper.cpp backends…")
	backends := whisper.Backends()
	hasMetal := false
	for _, b := range backends {
		// ggml registers the Metal backend as "MTL" (older builds: "Metal").
		if strings.EqualFold(b, "MTL") || strings.EqualFold(b, "Metal") {
			hasMetal = true
		}
	}
	if len(backends) == 0 {
		fail("engine", "whisper.cpp loaded but no compute backends found (brew reinstall ggml)")
	} else {
		note := "backends: " + strings.Join(backends, ", ")
		if !hasMetal && permissions.Needed() {
			note += "  (no Metal — will run on CPU, several times slower)"
		}
		pass("engine", "whisper.cpp — "+note)
	}

	// -- model ------------------------------------------------------------
	m, have := catalog.Get(cfg.Model)
	switch {
	case !have:
		fail("model", "none chosen — run: flowlite")
	case !m.Downloaded():
		fail("model", m.Label+" is not downloaded — flowlite settings → Speech model")
	default:
		p, _ := m.Path()
		pass("model", fmt.Sprintf("%s — %s", m.Label, shortenHome(p)))
		if doctorDeep {
			check("model load", "loading "+m.Label+"…")
			t0 := time.Now()
			model, err := whisper.Load(p)
			if err != nil {
				fail("model load", err.Error())
			} else {
				_, _ = model.Transcribe(make([]float32, audio.SampleRate), whisper.Options{})
				dev := "CPU"
				if whisper.UsingMetal() {
					dev = "Metal GPU (" + whisper.GPUName() + ")"
				}
				pass("model load", fmt.Sprintf("%s in %s", dev, time.Since(t0).Round(time.Millisecond)))
				model.Close()
			}
		}
	}

	if inst := catalog.Installed(); len(inst) > 1 {
		names := ""
		for _, im := range inst {
			names += " " + im.Key
		}
		fail("disk", fmt.Sprintf("%d models installed (%s) — FlowLite keeps one; re-choose yours under flowlite settings → Speech model", len(inst), names))
	}

	// -- permissions, as the background daemon sees them ------------------
	// On macOS the daemon runs under launchd, so macOS judges flowlite
	// itself, not this terminal: ask from there (see ownPermissions). With
	// --request this also shows the prompts.
	what := "checking flowlite's permissions…"
	if doctorRequest {
		what = "asking macOS for Accessibility and the microphone…"
	}
	check("permissions", what)
	ownAX, ownMic, permErr := ownPermissions(doctorRequest)
	spin.Stop()
	if permErr != nil {
		fail("permissions", permErr.Error())
	}

	// -- microphone -------------------------------------------------------
	// The permission first: listing devices does not need it, and neither does
	// opening one, so without this check every line below can pass while the
	// microphone delivers nothing but silence.
	if permErr == nil {
		if !ownMic.OK() {
			fail("microphone access", ownMic.Hint())
		} else {
			pass("microphone access", "granted to "+daemonApp())
		}
	}

	check("microphone", "listing audio devices…")
	devs, derr := audio.ListDevices()
	switch {
	case derr != nil:
		fail("microphone", derr.Error())
	case len(devs) == 0:
		fail("microphone", "no input devices found")
	default:
		name := audio.DefaultDeviceName()
		if cfg.InputDevice != "" {
			name = cfg.InputDevice
			found := false
			for _, d := range devs {
				if d.Name == name {
					found = true
				}
			}
			if !found {
				fail("microphone", fmt.Sprintf("%q is configured but not connected — flowlite settings → Microphone", name))
				name = ""
			}
		}
		if name != "" {
			pass("microphone", name)
		}
	}

	// -- clipboard --------------------------------------------------------
	check("clipboard", "read/write round-trip…")
	if err := inject.ClipboardRoundTrip(); err != nil {
		fail("clipboard", err.Error())
	} else {
		pass("clipboard", "read/write round-trip")
	}

	// -- paths ------------------------------------------------------------
	if d, err := config.Dir(); err != nil {
		fail("config dir", err.Error())
	} else if f, err := os.CreateTemp(d, ".probe"); err != nil {
		fail("config dir", d+" is not writable")
	} else {
		f.Close()
		os.Remove(f.Name())
		pass("config dir", shortenHome(d))
	}

	// -- keyboard permission (the one that actually bites) ---------------
	if permissions.Needed() && permErr == nil {
		if ownAX {
			pass("keyboard", "Accessibility granted to "+daemonApp())
		} else {
			fail("keyboard", bad("Accessibility is NOT granted to "+daemonApp()))
			printAccessibilityFix(doctorRequest)
		}
		// The terminal's own grant no longer matters for dictation; it only
		// lets `flowlite settings` detect a key you press to choose it.
		if runtime.GOOS == "darwin" {
			if permissions.Trusted() {
				info("terminal", dim(hostApp()+" has Accessibility too — settings can pick a key by pressing it"))
			} else {
				info("terminal", dim(hostApp()+" has no Accessibility — fine; only needed to pick a key by pressing it in settings"))
			}
		}
	}

	fmt.Println()
	if failed == 0 {
		fmt.Println(ok("Everything checks out."), dim("Start dictating with: flowlite"))
		return nil
	}
	fmt.Printf("%s\n", warn(fmt.Sprintf("%d problem(s) above.", failed)))
	return errSilent
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorRequest, "request", false, "show the macOS Accessibility and microphone prompts for flowlite")
	doctorCmd.Flags().BoolVar(&doctorDeep, "deep", false, "also load the model and report the GPU")
	rootCmd.AddCommand(doctorCmd)
}
