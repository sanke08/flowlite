package cli

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sanke08/flowlite/internal/audio"
	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
	"github.com/sanke08/flowlite/internal/history"
	"github.com/sanke08/flowlite/internal/hotkey"
	"github.com/sanke08/flowlite/internal/inject"
	"github.com/sanke08/flowlite/internal/overlay"
)

var settingsCmd = &cobra.Command{
	Use:   "settings",
	Short: "Everything you can change, in one menu",
	Long: `Everything you can change, in one menu.

Each row shows its current value; pick one, change it, and it is saved at
once. The menu also holds the things that are not settings but you reach for
now and then: test the microphone, copy a recent transcript, run FlowLite in
the background, reset, uninstall.`,
	Args: cobra.NoArgs,
	RunE: runSettingsMenu,
}

// menuItem is one row of the top-level menu.
type menuItem string

const (
	itemModel         menuItem = "model"
	itemKey           menuItem = "key"
	itemThreshold     menuItem = "threshold"
	itemPill          menuItem = "pill"
	itemMic           menuItem = "mic"
	itemLang          menuItem = "lang"
	itemSounds        menuItem = "sounds"
	itemTestMic       menuItem = "testmic"
	itemHistory       menuItem = "history"
	itemHistoryToggle menuItem = "historytoggle"
	itemDaemon        menuItem = "daemon"
	itemReset         menuItem = "reset"
	itemUninstall     menuItem = "uninstall"
	itemDone          menuItem = "done"
)

// languages is the short list offered before the free-text fallback. Codes
// are what whisper.cpp accepts; the order is roughly by how often FlowLite
// users ask for them.
var languages = []struct{ code, name string }{
	{"en", "English"},
	{"hi", "Hindi"},
	{"es", "Spanish"},
	{"fr", "French"},
	{"de", "German"},
	{"pt", "Portuguese"},
	{"it", "Italian"},
	{"ja", "Japanese"},
	{"zh", "Chinese"},
	{"ko", "Korean"},
	{"ru", "Russian"},
	{"ar", "Arabic"},
}

func runSettingsMenu(cmd *cobra.Command, args []string) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return errors.New("settings is interactive; run it in a terminal")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	dir, _ := config.Dir()
	fmt.Println(bold("FlowLite settings"))
	fmt.Println(dim("  FlowLite " + Version + " · " + shortenHome(dir) + " · ↑/↓ move · Enter choose · Esc back"))
	fmt.Println()

	// changed is set by rows the daemon cannot pick up live (it reads its
	// settings once, at start). One restart at the end, not per row.
	changed := false
	for {
		var pick menuItem
		if err := huh.NewSelect[menuItem]().
			Title("What do you want to do?").
			Options(menuOptions(cfg)...).
			Value(&pick).Run(); err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				break
			}
			return err
		}
		if pick == itemDone {
			break
		}
		res, err := runMenuItem(cfg, pick)
		if err != nil {
			if errors.Is(err, huh.ErrUserAborted) {
				fmt.Println()
				continue // Esc in a sub-prompt: back to the menu
			}
			return err
		}
		switch res {
		case resChanged:
			changed = true
		case resApplied:
			changed = false // the daemon was (re)started with the current settings
		case resQuit:
			return nil
		}
		fmt.Println()
	}
	if changed {
		if err := applyToRunningDaemon("your new settings"); err != nil {
			return err
		}
	}
	return nil
}

// menuResult is what a row reports back to the loop.
type menuResult int

const (
	resNothing menuResult = iota // no setting changed
	resChanged                   // a setting the daemon reads at start changed
	resApplied                   // the daemon was started/restarted: nothing pending
	resQuit                      // leave the menu now (uninstalled)
)

// menuOptions renders the menu rows with each setting's current value.
func menuOptions(cfg *config.Config) []huh.Option[menuItem] {
	row := func(name, value string) string {
		return fmt.Sprintf("%-20s %s", name, dim(value))
	}
	model := "none chosen"
	if m, has := catalog.Get(cfg.Model); has {
		model = m.Label
		if m.Downloaded() {
			model += " · " + catalog.Human(m.DiskBytes())
		} else {
			model += " (not downloaded)"
		}
	}
	return []huh.Option[menuItem]{
		huh.NewOption(row("Speech model", model), itemModel),
		huh.NewOption(row("Dictation key", hotkey.Label(cfg.Hotkey)), itemKey),
		huh.NewOption(row("Hold threshold", fmt.Sprintf("%d ms", cfg.HoldThresholdMS)), itemThreshold),
		huh.NewOption(row("Pill position", cfg.PillPosition), itemPill),
		huh.NewOption(row("Microphone", micValue(cfg)), itemMic),
		huh.NewOption(row("Language", orAuto(cfg.Language)), itemLang),
		huh.NewOption(row("Sounds", onOff(cfg.Sounds)), itemSounds),
		huh.NewOption(row("Test microphone", "record 4 s, print the transcript"), itemTestMic),
		huh.NewOption(row("Recent transcripts", historyValue()), itemHistory),
		huh.NewOption(row("Remember transcripts", onOff(cfg.HistoryEnabled)), itemHistoryToggle),
		huh.NewOption(row("Background daemon", daemonStatus()), itemDaemon),
		huh.NewOption(row("Reset to defaults", ""), itemReset),
		huh.NewOption(row("Uninstall FlowLite", ""), itemUninstall),
		huh.NewOption("Done", itemDone),
	}
}

func micValue(cfg *config.Config) string {
	if cfg.InputDevice != "" {
		return cfg.InputDevice
	}
	if d := audio.DefaultDeviceName(); d != "" {
		return "system default (" + d + ")"
	}
	return "system default"
}

func historyValue() string {
	store, err := history.Open()
	if err != nil {
		return "unavailable"
	}
	all, err := store.List(0)
	if err != nil || len(all) == 0 {
		return "none yet"
	}
	return fmt.Sprintf("%d kept · last %s", len(all), all[0].Time.Local().Format("15:04"))
}

// runMenuItem runs one row.
func runMenuItem(cfg *config.Config, item menuItem) (menuResult, error) {
	asResult := func(did bool, err error) (menuResult, error) {
		if did {
			return resChanged, err
		}
		return resNothing, err
	}
	switch item {
	case itemModel:
		return asResult(changeModel(cfg))
	case itemKey:
		return asResult(changeKey(cfg))
	case itemThreshold:
		return asResult(changeThreshold(cfg))
	case itemPill:
		return asResult(changePill(cfg))
	case itemMic:
		return asResult(changeMic(cfg))
	case itemLang:
		return asResult(changeLanguage(cfg))
	case itemSounds:
		return asResult(changeSounds(cfg))
	case itemTestMic:
		return resNothing, testMicrophone(cfg)
	case itemHistory:
		return resNothing, copyTranscript(cfg)
	case itemHistoryToggle:
		return asResult(changeHistoryEnabled(cfg))
	case itemDaemon:
		return daemonMenu()
	case itemReset:
		return asResult(resetDefaults(cfg))
	case itemUninstall:
		gone, err := uninstallFlowLite()
		if gone {
			return resQuit, err
		}
		return resNothing, err
	}
	return resNothing, nil
}

// save persists cfg and prints a one-line confirmation.
func save(cfg *config.Config, what, value string) (bool, error) {
	if err := cfg.Save(); err != nil {
		return false, err
	}
	fmt.Printf("%s %s: %s\n", ok("✓"), what, value)
	return true, nil
}

// ---- rows 1–7: settings ------------------------------------------------------

func changeModel(cfg *config.Config) (bool, error) {
	opts := make([]huh.Option[string], 0, len(catalog.Catalog))
	for _, m := range catalog.Catalog {
		label := fmt.Sprintf("%-30s %8s", m.Label, catalog.Human(m.SizeBytes))
		switch {
		case m.Downloaded() && m.Key == cfg.Model:
			label += "   " + ok("● installed")
		case m.Downloaded():
			label += "   installed"
		case m.Recommended:
			label += "   recommended"
		}
		opts = append(opts, huh.NewOption(label, m.Key))
	}
	key := cfg.Model
	if key == "" {
		key = catalog.Default().Key
	}
	if err := huh.NewSelect[string]().
		Title("Speech model").
		Description("FlowLite keeps one model on disk: choosing another downloads it, then removes the current one.").
		Options(opts...).Value(&key).Run(); err != nil {
		return false, err
	}
	m, _ := catalog.Get(key)
	if key == cfg.Model && m.Downloaded() {
		fmt.Println(dim("  already the active model"))
		return false, nil
	}
	// switchModel downloads, saves, prunes and prints its own confirmation.
	return true, switchModel(cfg, m)
}

func changeKey(cfg *config.Config) (bool, error) {
	opts := make([]huh.Option[string], 0)
	for _, n := range hotkey.Names() {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-16s %s", hotkey.Label(n), dim(n)), n))
	}
	key := cfg.Hotkey
	if err := huh.NewSelect[string]().
		Title("Dictation key").
		Description("Hold to talk · double-tap for hands-free, press again to stop · triple-tap pastes your last transcript · Esc cancels.").
		Options(opts...).Value(&key).Run(); err != nil {
		return false, err
	}
	if key == cfg.Hotkey {
		return false, nil
	}
	cfg.Hotkey = key
	return save(cfg, "dictation key", hotkey.Label(key))
}

const (
	minHoldMS = 150
	maxHoldMS = 900
)

// validHold accepts an integer number of milliseconds within the range that
// still lets taps and holds be told apart.
func validHold(s string) error {
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "ms")))
	if err != nil {
		return errors.New("a whole number of milliseconds, like 400")
	}
	if n < minHoldMS || n > maxHoldMS {
		return fmt.Errorf("between %d and %d", minHoldMS, maxHoldMS)
	}
	return nil
}

func changeThreshold(cfg *config.Config) (bool, error) {
	val := strconv.Itoa(cfg.HoldThresholdMS)
	if err := huh.NewInput().
		Title("Hold threshold (ms)").
		Description(fmt.Sprintf("A press shorter than this is a tap, longer is a hold. %d–%d; the default 400 suits most people.", minHoldMS, maxHoldMS)).
		Validate(validHold).
		Value(&val).Run(); err != nil {
		return false, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(val), "ms")))
	if n == cfg.HoldThresholdMS {
		return false, nil
	}
	cfg.HoldThresholdMS = n
	return save(cfg, "hold threshold", fmt.Sprintf("%d ms", n))
}

func changePill(cfg *config.Config) (bool, error) {
	opts := make([]huh.Option[string], 0, len(overlay.Positions))
	for _, p := range overlay.Positions {
		opts = append(opts, huh.NewOption(p, p))
	}
	pos := cfg.PillPosition
	if err := huh.NewSelect[string]().
		Title("Pill position").
		Description("The screen edge the pill sits on, centred and 100 px in. On the left and right it stands upright.").
		Options(opts...).Value(&pos).Run(); err != nil {
		return false, err
	}
	if !overlay.ValidPosition(pos) {
		return false, nil
	}
	preview := true
	if err := huh.NewConfirm().
		Title("Preview?").
		Description("Runs one pretend dictation at the " + pos + ": the pill, its animation and the sound cues.").
		Affirmative("Yes").Negative("No").Value(&preview).Run(); err != nil {
		return false, err
	}
	if preview {
		if err := runSelf("--pill-preview", pos); err != nil {
			fmt.Println(warn("  preview failed: " + err.Error()))
		}
	}
	if pos == cfg.PillPosition {
		return false, nil
	}
	cfg.PillPosition = pos
	return save(cfg, "pill position", pos)
}

func changeMic(cfg *config.Config) (bool, error) {
	spin := startSpinner("Listing microphones…")
	devs, err := audio.ListDevices()
	spin.Stop()
	if err != nil {
		return false, err
	}
	opts := []huh.Option[string]{huh.NewOption("system default", "")}
	for _, d := range devs {
		label := d.Name
		if d.IsDefault {
			label += "  " + dim("(current system default)")
		}
		opts = append(opts, huh.NewOption(label, d.Name))
	}
	dev := cfg.InputDevice
	if err := huh.NewSelect[string]().
		Title("Microphone").
		Description("\"system default\" follows whatever macOS has selected, so AirPods and headsets just work.").
		Options(opts...).Value(&dev).Run(); err != nil {
		return false, err
	}
	if dev == cfg.InputDevice {
		return false, nil
	}
	cfg.InputDevice = dev
	return save(cfg, "microphone", orDefault(dev))
}

func changeLanguage(cfg *config.Config) (bool, error) {
	const other = "\x00other"
	opts := []huh.Option[string]{huh.NewOption(fmt.Sprintf("%-16s %s", "auto-detect", dim("whisper decides per dictation")), "")}
	for _, l := range languages {
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-16s %s", l.name, dim(l.code)), l.code))
	}
	opts = append(opts, huh.NewOption("Other… (type a code)", other))
	lang := cfg.Language
	if err := huh.NewSelect[string]().
		Title("Language").
		Description("Fixing the language skips detection and helps with short phrases. English-only models ignore this.").
		Options(opts...).Value(&lang).Run(); err != nil {
		return false, err
	}
	if lang == other {
		lang = cfg.Language
		if err := huh.NewInput().
			Title("Language code").
			Description("An ISO 639 code whisper.cpp knows: nl, sv, tr, pl, uk, ta, bn…  Empty means auto-detect.").
			Placeholder("nl").
			Validate(validLanguage).
			Value(&lang).Run(); err != nil {
			return false, err
		}
		lang = strings.ToLower(strings.TrimSpace(lang))
		if lang == "auto" {
			lang = ""
		}
	}
	if lang == cfg.Language {
		return false, nil
	}
	cfg.Language = lang
	return save(cfg, "language", orAuto(lang))
}

// validLanguage accepts empty/auto or a 2–3 letter code.
func validLanguage(s string) error {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" || s == "auto" {
		return nil
	}
	if len(s) < 2 || len(s) > 3 {
		return errors.New("use a 2–3 letter code like en, hi, es — or leave empty for auto")
	}
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return errors.New("letters only, like en, hi, es")
		}
	}
	return nil
}

func changeSounds(cfg *config.Config) (bool, error) {
	const play = "play"
	choice := onOff(cfg.Sounds)
	if err := huh.NewSelect[string]().
		Title("Sounds").
		Description("A short cue when recording starts, stops, pastes or fails.").
		Options(
			huh.NewOption("On", "on"),
			huh.NewOption("Off", "off"),
			huh.NewOption("Play the cues", play),
		).Value(&choice).Run(); err != nil {
		return false, err
	}
	if choice == play {
		if err := runSelf("--play-cues"); err != nil {
			fmt.Println(warn("  could not play: " + err.Error()))
		}
		return false, nil
	}
	on := choice == "on"
	if on == cfg.Sounds {
		return false, nil
	}
	cfg.Sounds = on
	return save(cfg, "sounds", onOff(on))
}

func changeHistoryEnabled(cfg *config.Config) (bool, error) {
	choice := onOff(cfg.HistoryEnabled)
	if err := huh.NewSelect[string]().
		Title("Remember transcripts").
		Description("Keeps recent transcripts so a triple-tap or \"Recent transcripts\" can recover one. Off means nothing new is recorded from here on — existing history stays until cleared.").
		Options(
			huh.NewOption("On", "on"),
			huh.NewOption("Off", "off"),
		).Value(&choice).Run(); err != nil {
		return false, err
	}
	on := choice == "on"
	if on == cfg.HistoryEnabled {
		return false, nil
	}
	cfg.HistoryEnabled = on
	return save(cfg, "remember transcripts", onOff(on))
}

// ---- rows 9–11: things that are not settings ---------------------------------

// copyTranscript lists the last ten transcripts and puts the chosen one on
// the clipboard. Copy rather than paste: the terminal has focus here, so a
// paste would land in the wrong place.
func copyTranscript(cfg *config.Config) error {
	store, err := history.Open()
	if err != nil {
		return err
	}
	entries, err := store.List(10)
	if err != nil {
		return fmt.Errorf("read history: %w", err)
	}
	if len(entries) == 0 {
		fmt.Println(dim("  No transcripts yet — run flowlite and dictate something."))
		return nil
	}
	opts := make([]huh.Option[int], 0, len(entries)+1)
	for i, e := range entries {
		text := oneLine(e.Text)
		if r := []rune(text); len(r) > 60 {
			text = string(r[:60]) + "…"
		}
		label := fmt.Sprintf("%s  %s", dim(e.Time.Local().Format("15:04")), text)
		opts = append(opts, huh.NewOption(label, i))
	}
	const clearAll = -2
	opts = append(opts, huh.NewOption(warn("Clear all history"), clearAll))
	opts = append(opts, huh.NewOption(dim("← back"), -1))
	pick := 0
	if err := huh.NewSelect[int]().
		Title("Recent transcripts").
		Description("Pick one to copy it.").
		Options(opts...).Value(&pick).Run(); err != nil {
		return err
	}
	if pick == clearAll {
		confirm := false
		if err := huh.NewConfirm().
			Title("Clear all history?").
			Description("Permanently erases every remembered transcript. This cannot be undone.").
			Affirmative("Clear").Negative("Keep").Value(&confirm).Run(); err != nil {
			return err
		}
		if !confirm {
			return nil
		}
		if err := store.Clear(); err != nil {
			return fmt.Errorf("clear history: %w", err)
		}
		fmt.Printf("%s history cleared\n", ok("✓"))
		return nil
	}
	if pick < 0 {
		return nil
	}
	if err := inject.SetClipboard(entries[pick].Text); err != nil {
		return fmt.Errorf("copy to clipboard: %w", err)
	}
	pasteKey := "⌘V"
	if runtime.GOOS == "windows" {
		pasteKey = "Ctrl+V"
	}
	fmt.Printf("%s copied — %s where you want it · triple-tap %s in any field pastes the most recent\n",
		ok("✓"), pasteKey, hotkey.Label(cfg.Hotkey))
	return nil
}

// oneLine collapses a transcript onto a single line.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// daemonMenu is row 10: start, stop or restart the background daemon. The
// foreground (`flowlite` in a tab) stays the recommended way to run.
func daemonMenu() (menuResult, error) {
	const (
		actStart   = "start"
		actStop    = "stop"
		actRestart = "restart"
		actBack    = "back"
	)
	_, running := daemonRunning()
	var opts []huh.Option[string]
	desc := "FlowLite always runs in the background. Stop it, start it, or restart it here."
	if running {
		opts = []huh.Option[string]{
			huh.NewOption("Stop", actStop),
			huh.NewOption("Restart (apply changed settings)", actRestart),
		}
		desc = "Stops whichever FlowLite is listening — in the background or in another window."
	} else {
		opts = []huh.Option[string]{huh.NewOption("Start in background", actStart)}
	}
	opts = append(opts, huh.NewOption(dim("← back"), actBack))
	act := opts[0].Value
	if err := huh.NewSelect[string]().
		Title("Background daemon · " + daemonStatus()).
		Description(desc).
		Options(opts...).Value(&act).Run(); err != nil {
		return resNothing, err
	}
	switch act {
	case actStart:
		return resApplied, startBackground()
	case actStop:
		return resNothing, stopBackground()
	case actRestart:
		if err := stopBackground(); err != nil {
			return resNothing, err
		}
		return resApplied, startBackground()
	}
	return resNothing, nil
}

// resetDefaults rewrites every setting to its default. The model stays: it
// is the one thing that took time to get, and "defaults" should not mean a
// 547 MB download.
func resetDefaults(cfg *config.Config) (bool, error) {
	confirm := false
	if err := huh.NewConfirm().
		Title("Reset every setting to its default?").
		Description("Key, hold threshold, pill position, microphone, language and sounds go back to how they shipped. The speech model on disk is kept.").
		Affirmative("Reset").Negative("Keep").Value(&confirm).Run(); err != nil {
		return false, err
	}
	if !confirm {
		return false, nil
	}
	def := config.Default()
	def.Model = cfg.Model
	*cfg = *def
	return save(cfg, "settings", "reset to defaults (model kept)")
}

// runSelf runs this binary again with args, inheriting the terminal, and
// waits. The pill and the audio device both want a fresh process.
func runSelf(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	c := exec.Command(exe, args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// ---- small formatters --------------------------------------------------------

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

func init() {
	rootCmd.AddCommand(settingsCmd)
}
