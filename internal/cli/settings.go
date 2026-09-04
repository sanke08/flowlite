package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/hotkey"
)

var keyCmd = &cobra.Command{
	Use:   "key [name]",
	Short: "Show or set the dictation key",
	Long: `Show or set the dictation key.

Valid keys do nothing on their own and are rarely bound by other software:
  ` + strings.Join(hotkey.Names(), "  "),
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Printf("dictation key: %s (%s)\n", hotkey.Label(cfg.Hotkey), cfg.Hotkey)
			fmt.Println(dim("  tap to start and stop · hold to dictate while pressed · Esc cancels"))
			return nil
		}
		name := strings.ToLower(args[0])
		if !hotkey.Valid(name) {
			return fmt.Errorf("%q is not a supported key — choose one of: %s", args[0], strings.Join(hotkey.Names(), ", "))
		}
		cfg.Hotkey = name
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("%s dictation key: %s\n", ok("✓"), hotkey.Label(name))
		restartHint()
		return nil
	},
}

var micCmd = &cobra.Command{
	Use:   "mic [list|<name>|default]",
	Short: "Show, list or choose the microphone",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		devs, derr := audio.ListDevices()
		if len(args) == 0 {
			cur := cfg.InputDevice
			if cur == "" {
				cur = "system default"
				if d := audio.DefaultDeviceName(); d != "" {
					cur += " (" + d + ")"
				}
			}
			fmt.Println("microphone:", cur)
			return nil
		}
		switch args[0] {
		case "list":
			if derr != nil {
				return derr
			}
			for _, d := range devs {
				mark := "  "
				if d.Name == cfg.InputDevice || (cfg.InputDevice == "" && d.IsDefault) {
					mark = ok("●") + " "
				}
				def := ""
				if d.IsDefault {
					def = dim("  (system default)")
				}
				fmt.Printf("  %s %s%s\n", mark, d.Name, def)
			}
			return nil
		case "default":
			cfg.InputDevice = ""
		default:
			found := false
			for _, d := range devs {
				if strings.EqualFold(d.Name, args[0]) {
					cfg.InputDevice = d.Name
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("no microphone named %q — see `flowlite mic list`", args[0])
			}
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("%s microphone: %s\n", ok("✓"), orDefault(cfg.InputDevice))
		restartHint()
		return nil
	},
}

var langCmd = &cobra.Command{
	Use:   "lang [code|auto]",
	Short: "Show or set the spoken language (ISO code like en, hi, es — or auto)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Println("language:", orAuto(cfg.Language))
			return nil
		}
		v := strings.ToLower(args[0])
		if v == "auto" {
			v = ""
		}
		if v != "" && (len(v) < 2 || len(v) > 3) {
			return fmt.Errorf("%q does not look like a language code (try en, hi, es, fr… or auto)", args[0])
		}
		cfg.Language = v
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("%s language: %s\n", ok("✓"), orAuto(v))
		restartHint()
		return nil
	},
}

var soundsCmd = &cobra.Command{
	Use:   "sounds [on|off]",
	Short: "Turn the audio cues on or off",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := loadConfig()
		if err != nil {
			return err
		}
		if len(args) == 0 {
			fmt.Println("sounds:", onOff(cfg.Sounds))
			return nil
		}
		switch strings.ToLower(args[0]) {
		case "on":
			cfg.Sounds = true
		case "off":
			cfg.Sounds = false
		default:
			return fmt.Errorf("say on or off")
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Printf("%s sounds: %s\n", ok("✓"), onOff(cfg.Sounds))
		restartHint()
		return nil
	},
}

func orDefault(s string) string {
	if s == "" {
		return "system default"
	}
	return s
}

func orAuto(s string) string {
	if s == "" {
		return "auto-detect"
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func restartHint() {
	if _, running := daemonRunning(); running {
		fmt.Println(dim("  the running daemon keeps its old settings until: flowlite stop && flowlite start"))
	}
}

func init() {
	rootCmd.AddCommand(keyCmd, micCmd, langCmd, soundsCmd)
}
