package cli

import (
	"fmt"
	"os"
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
	RunE:  runDoctor,
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	failed := 0
	pass := func(label, detail string) {
		fmt.Printf("  %s %-14s %s\n", ok("✓"), label, dim(detail))
	}
	fail := func(label, detail string) {
		failed++
		fmt.Printf("  %s %-14s %s\n", bad("✗"), label, detail)
	}

	fmt.Println(bold("FlowLite doctor"))
	fmt.Println()

	// -- speech engine --------------------------------------------------
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
		fail("model", "none chosen — run: flowlite setup")
	case !m.Downloaded():
		fail("model", m.Label+" is not downloaded — run: flowlite download "+m.Key)
	default:
		p, _ := m.Path()
		pass("model", fmt.Sprintf("%s — %s", m.Label, shortenHome(p)))
		if doctorDeep {
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

	// -- microphone -------------------------------------------------------
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
				fail("microphone", fmt.Sprintf("%q is configured but not connected — flowlite mic default", name))
				name = ""
			}
		}
		if name != "" {
			pass("microphone", name)
		}
	}

	// -- clipboard --------------------------------------------------------
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
	if permissions.Needed() {
		if doctorRequest && !permissions.Trusted() {
			permissions.Request()
			time.Sleep(300 * time.Millisecond)
		}
		if permissions.Trusted() {
			pass("keyboard", "Accessibility granted to "+hostApp())
		} else {
			fail("keyboard", bad("Accessibility is NOT granted to "+hostApp()))
			fmt.Println()
			fmt.Println("     Without it macOS never delivers the dictation key to FlowLite, so")
			fmt.Println("     pressing it does nothing at all. This is the step most people miss.")
			fmt.Println()
			if !doctorRequest {
				fmt.Println("       1.", blue("flowlite doctor --request"), dim("— opens the macOS prompt and adds "+hostApp()+" to the list"))
			} else {
				fmt.Println("       1.", dim("a macOS prompt should have appeared; it added "+hostApp()+" to the list"))
			}
			fmt.Println("       2. System Settings → Privacy & Security → Accessibility → switch on", bold(hostApp()))
			fmt.Println("       3.", blue("flowlite doctor"), dim("again to confirm, then"), blue("flowlite run"))
			fmt.Println()
			fmt.Println(dim("     The permission attaches to " + hostApp() + " — the app that launched flowlite —"))
			fmt.Println(dim("     not to the flowlite binary, so it survives every rebuild."))
		}
	}

	fmt.Println()
	if failed == 0 {
		fmt.Println(ok("Everything checks out."), dim("Start dictating with: flowlite run"))
		return nil
	}
	fmt.Printf("%s\n", warn(fmt.Sprintf("%d problem(s) above.", failed)))
	os.Exit(1)
	return nil
}

func shortenHome(p string) string {
	if h, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, h) {
		return "~" + p[len(h):]
	}
	return p
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorRequest, "request", false, "trigger the macOS Accessibility prompt")
	doctorCmd.Flags().BoolVar(&doctorDeep, "deep", false, "also load the model and report the GPU")
	rootCmd.AddCommand(doctorCmd)
}
