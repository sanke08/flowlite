package cli

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/sanke08/flowlite/internal/config"
)

// releasesLatest is the only network endpoint the updater talks to. GitHub's
// "latest" excludes pre-releases and drafts, so what it returns is always
// something a user should be offered.
const releasesLatest = "https://api.github.com/repos/sanke08/flowlite/releases/latest"

// releasesPage is where to send people when we cannot update in place.
const releasesPage = "https://github.com/sanke08/flowlite/releases"

// noUpdateCheckEnv, when set to anything, silences the once-a-day notice.
// The explicit `flowlite update` still works.
const noUpdateCheckEnv = "FLOWLITE_NO_UPDATE_CHECK"

// notifyEvery is how often the once-a-day notice (root banner, doctor) may
// ask GitHub whether there is something newer. gh uses the same interval.
const notifyEvery = 24 * time.Hour

var (
	updateCheckOnly bool
	updateTo        string
)

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update flowlite to the latest release",
	Long: `Update flowlite to the latest release.

Downloads the release for this machine from GitHub, checks it is complete,
and replaces the running binary in place. Your settings and model are
untouched. A running daemon is restarted automatically onto the new version.`,
	Args: cobra.NoArgs,
	RunE: runUpdate,
}

// release is the subset of GitHub's release object the updater needs.
type release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	URL  string `json:"browser_download_url"`
}

// assetSuffix is the tail of the release asset built for this OS/arch, e.g.
// flowlite-v0.4.0-macos-arm64 or flowlite-v0.4.0-windows-x64.zip.
func assetSuffix(goos, goarch string) (string, error) {
	switch goos + "/" + goarch {
	case "darwin/arm64":
		return "macos-arm64", nil
	case "windows/amd64":
		return "windows-x64.zip", nil
	}
	return "", fmt.Errorf("no release is published for %s/%s", goos, goarch)
}

// assetFor picks the asset for this platform out of a release.
func (r *release) assetFor(goos, goarch string) (asset, error) {
	suffix, err := assetSuffix(goos, goarch)
	if err != nil {
		return asset{}, err
	}
	for _, a := range r.Assets {
		if strings.HasSuffix(a.Name, "-"+suffix) {
			return a, nil
		}
	}
	return asset{}, fmt.Errorf("release %s has no %s asset — see %s", r.TagName, suffix, releasesPage)
}

// checksumAssetName is the file the release workflow's "Checksums" step
// publishes alongside every build (`shasum -a 256 flowlite-* > SHA256SUMS`):
// one sha256 line per asset, in standard shasum format.
const checksumAssetName = "SHA256SUMS"

// checksumFor downloads the release's SHA256SUMS asset and returns the hex
// digest it lists for assetName (matched against the original asset
// filename, e.g. the Windows zip — not the exe later extracted from it).
//
// A release that does not publish SHA256SUMS, or one where the file has no
// line for this asset, is treated as a hard failure rather than a silent
// skip: this check exists specifically to keep an unverified binary from
// ever being executed or installed, so "cannot verify" must behave the same
// as "verification failed".
func checksumFor(ctx context.Context, rel *release, assetName string) (string, error) {
	var sums asset
	found := false
	for _, a := range rel.Assets {
		if a.Name == checksumAssetName {
			sums = a
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("release %s does not publish %s — refusing to install an unverified binary; see %s", rel.TagName, checksumAssetName, releasesPage)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sums.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "flowlite/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading %s: %w", checksumAssetName, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("downloading %s: HTTP %d", checksumAssetName, resp.StatusCode)
	}

	scanner := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && fields[1] == assetName {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", checksumAssetName, err)
	}
	return "", fmt.Errorf("%s has no entry for %s — refusing to install an unverified binary; see %s", checksumAssetName, assetName, releasesPage)
}

// fetchLatest asks GitHub for the newest release.
func fetchLatest(ctx context.Context) (*release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesLatest, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "flowlite/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			return nil, errors.New("GitHub API rate limit reached — try again in an hour, or download from " + releasesPage)
		}
		return nil, fmt.Errorf("GitHub returned HTTP %d for %s", resp.StatusCode, releasesLatest)
	}
	var r release
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&r); err != nil {
		return nil, fmt.Errorf("reading release info: %w", err)
	}
	if r.TagName == "" {
		return nil, errors.New("release info has no tag name")
	}
	return &r, nil
}

// version is the part of a version string that decides ordering. A build the
// Makefile marks -dev+<sha> is "on the way to" its base version, so it sorts
// just below the release with the same numbers: v0.4.0-dev+abc < v0.4.0.
type version struct {
	major, minor, patch int
	dev                 bool
}

// parseVersion accepts v0.3.1, 0.3.1, v0.4.0-dev+abc1234, v0.4.0-rc1. The
// bare "dev" the source tree builds with parses as 0.0.0-dev: older than
// everything, which is the honest answer for an unversioned build.
func parseVersion(s string) (version, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return version{}, false
	}
	if s == "dev" {
		return version{dev: true}, true
	}
	var v version
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		v.dev = true
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	nums := [3]int{}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		nums[i] = n
	}
	v.major, v.minor, v.patch = nums[0], nums[1], nums[2]
	return v, true
}

// compare orders two versions: -1 if a < b, 0 if equal, 1 if a > b.
func (a version) compare(b version) int {
	for _, d := range []int{a.major - b.major, a.minor - b.minor, a.patch - b.patch} {
		switch {
		case d < 0:
			return -1
		case d > 0:
			return 1
		}
	}
	switch {
	case a.dev && !b.dev:
		return -1
	case !a.dev && b.dev:
		return 1
	}
	return 0
}

// isNewer reports whether latest should replace current. Unparseable strings
// never trigger an update.
func isNewer(latest, current string) bool {
	l, ok1 := parseVersion(latest)
	c, ok2 := parseVersion(current)
	return ok1 && ok2 && l.compare(c) > 0
}

// targetPath is the file the update replaces: the running executable with
// symlinks resolved, or whatever --to points at.
func targetPath() (string, error) {
	if updateTo != "" {
		return filepath.Abs(updateTo)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

func runUpdate(cmd *cobra.Command, args []string) error {
	target, err := targetPath()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	spin := startSpinner("Checking GitHub for the latest release…")
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	rel, err := fetchLatest(rctx)
	cancel()
	spin.Stop()
	if err != nil {
		return fmt.Errorf("checking for updates: %w", err)
	}
	_ = writeCheckCache(checkCache{CheckedAt: time.Now(), Latest: rel.TagName})

	if !isNewer(rel.TagName, Version) {
		if updateCheckOnly || Version == rel.TagName {
			fmt.Printf("%s flowlite %s is up to date\n", ok("✓"), Version)
		} else {
			fmt.Printf("%s flowlite %s is already newer than the latest release (%s)\n", ok("✓"), Version, rel.TagName)
		}
		return nil
	}
	if updateCheckOnly {
		fmt.Printf("%s available (you have %s) — run: %s\n", bold(rel.TagName), Version, blue("flowlite update"))
		return nil
	}

	a, err := rel.assetFor(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	if err := checkWritable(target); err != nil {
		return err
	}

	fmt.Printf("Updating %s → %s\n", dim(Version), bold(rel.TagName))
	fmt.Println(dim("  " + target))
	restart := restartForUpdate()
	if err := applyUpdate(ctx, target, rel, a, restart); err != nil {
		if ctx.Err() != nil {
			return errors.New("interrupted — nothing was changed")
		}
		return err
	}
	fmt.Printf("%s updated %s → %s\n", ok("✓"), Version, rel.TagName)
	if restart {
		return nil // applyUpdate already brought the new version up
	}
	if err := applyToRunningDaemon("the new version"); err != nil {
		return err
	}
	return nil
}

// restartForUpdate reports whether applyUpdate should stop the running
// daemon before swapping the binary and start it again after. Only Windows
// needs this: it locks a running .exe against replacement, where Unix lets
// the old inode live on under the running process. The user can turn it off
// (settings → Restart for updates) and move the file into place by hand.
func restartForUpdate() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	cfg, err := loadConfig()
	if err != nil || !cfg.UpdateRestart {
		return false
	}
	_, running := daemonRunning()
	// A foreground `--no-paste` daemon lives in someone's terminal; stopping
	// it and respawning a detached one would silently change its mode.
	// Leave it alone and let the manual step apply.
	return running && !daemonIsForeground()
}

// checkWritable fails early, before any download, when the target's
// directory cannot take a new file (a root-owned /usr/local/bin, say).
func checkWritable(target string) error {
	dir := filepath.Dir(target)
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("cannot update %s: %w", target, err)
	}
	f, err := os.CreateTemp(dir, ".flowlite-probe")
	if err != nil {
		return fmt.Errorf("%s is not writable — re-run with sudo, or reinstall: curl -fsSL https://raw.githubusercontent.com/sanke08/flowlite/main/install.sh | sh", dir)
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

// applyUpdate downloads a into a temp file beside target, verifies it
// against the release's published SHA256SUMS, and swaps it in. The temp file
// lives in the same directory so the final rename is a single atomic step:
// at no point is there a half-written flowlite on PATH.
//
// restartDaemon (Windows, see restartForUpdate) stops the running daemon
// gracefully just before the swap — after the download and checksum, so the
// daemon is down only for the rename itself — and starts it again afterwards,
// whether or not the swap succeeded, so a failed update never leaves FlowLite
// off.
func applyUpdate(ctx context.Context, target string, rel *release, a asset, restartDaemon bool) error {
	dir := filepath.Dir(target)
	tmp, err := os.CreateTemp(dir, ".flowlite-update-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { os.Remove(tmpPath) }

	// Fetched before spending time on the download itself: if the release
	// has nothing to verify against, there is no point pulling the asset at
	// all.
	wantSum, err := checksumFor(ctx, rel, a.Name)
	if err != nil {
		tmp.Close()
		cleanup()
		return err
	}

	if err := downloadTo(ctx, tmp, a, wantSum); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}

	if strings.HasSuffix(a.Name, ".zip") {
		// The Windows asset is a zip holding flowlite.exe plus its DLLs; only
		// the executable is replaced. DLLs change rarely and are versioned in
		// the release notes when they do.
		unzipped, err := extractExe(tmpPath, dir)
		cleanup()
		if err != nil {
			return err
		}
		tmpPath = unzipped
		cleanup = func() { os.Remove(tmpPath) }
	}

	if err := os.Chmod(tmpPath, 0o755); err != nil {
		cleanup()
		return err
	}
	if runtime.GOOS == "darwin" {
		// Go's HTTP client never sets the quarantine flag, but the user may
		// have a tool that tags everything new in that directory; clearing
		// it is free and never harmful.
		_ = exec.Command("xattr", "-d", "com.apple.quarantine", tmpPath).Run()
	}
	if runtime.GOOS != "windows" {
		if err := sanityCheck(tmpPath); err != nil {
			cleanup()
			return err
		}
	}
	if restartDaemon {
		fmt.Println(dim("  stopping FlowLite to replace the binary…"))
		if err := stopBackground(); err != nil {
			cleanup()
			if errors.Is(err, errForcedStop) {
				// The daemon had to be killed, most likely mid-transcription.
				// Do not compound that by swapping the binary underneath a
				// state we did not see end cleanly: bring FlowLite back on
				// the current version and let the user retry when idle.
				if err2 := startBackground(); err2 != nil {
					return fmt.Errorf("FlowLite did not stop cleanly and was killed (a transcription in flight was likely lost); the update was not applied, and restarting the old version failed: %w", err2)
				}
				return errors.New("FlowLite did not stop cleanly and was killed — a transcription in flight was likely lost. The update was not applied and the previous version is running again; retry `flowlite update` when FlowLite is idle")
			}
			return fmt.Errorf("could not stop the running FlowLite: %w — nothing was changed", err)
		}
	}
	replaceErr := replaceBinary(tmpPath, target)
	if replaceErr != nil {
		cleanup()
	}
	if restartDaemon {
		if err := startBackground(); err != nil {
			return errors.Join(replaceErr, err)
		}
	}
	return replaceErr
}

// downloadTo streams the asset into f with a progress bar, refuses anything
// whose length does not match what the release API published, and — the
// check that actually matters, since sizes alone prove nothing about
// content — refuses anything whose sha256 does not match wantSHA256 (from
// the release's SHA256SUMS asset). This must run before the caller ever
// chmods, xattrs, executes (sanityCheck), or installs the file.
func downloadTo(ctx context.Context, f *os.File, a asset, wantSHA256 string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "flowlite/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", a.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: HTTP %d", a.Name, resp.StatusCode)
	}
	if resp.ContentLength > 0 && a.Size > 0 && resp.ContentLength != a.Size {
		return fmt.Errorf("%s: server offers %d bytes but the release lists %d — refusing", a.Name, resp.ContentLength, a.Size)
	}

	hash := sha256.New()
	var w io.Writer = io.MultiWriter(f, hash)
	if term.IsTerminal(int(os.Stderr.Fd())) {
		bar := progressbar.NewOptions64(a.Size,
			progressbar.OptionSetDescription("  "+a.Name),
			progressbar.OptionShowBytes(true),
			progressbar.OptionSetWidth(28),
			progressbar.OptionThrottle(65*time.Millisecond),
			progressbar.OptionClearOnFinish(),
			progressbar.OptionSetRenderBlankState(true),
			progressbar.OptionSetWriter(os.Stderr),
		)
		defer func() { _ = bar.Finish() }()
		w = io.MultiWriter(f, hash, bar)
	}
	n, err := io.Copy(w, io.LimitReader(resp.Body, a.Size+1))
	if err != nil {
		return fmt.Errorf("downloading %s: %w", a.Name, err)
	}
	if n != a.Size {
		return fmt.Errorf("%s: got %d bytes, release lists %d — download incomplete or altered", a.Name, n, a.Size)
	}
	if wantSHA256 != "" {
		if sum := hex.EncodeToString(hash.Sum(nil)); sum != wantSHA256 {
			return fmt.Errorf("%s failed checksum verification — the download is corrupt or was tampered with; run the same command again", a.Name)
		}
	}
	return nil
}

// extractExe pulls flowlite.exe out of the Windows zip into a temp file in
// dir and returns its path.
func extractExe(zipPath, dir string) (string, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return "", fmt.Errorf("opening %s: %w", filepath.Base(zipPath), err)
	}
	defer zr.Close()
	for _, zf := range zr.File {
		if filepath.Base(zf.Name) != "flowlite.exe" {
			continue
		}
		in, err := zf.Open()
		if err != nil {
			return "", err
		}
		defer in.Close()
		out, err := os.CreateTemp(dir, ".flowlite-update-*.exe")
		if err != nil {
			return "", err
		}
		if _, err := io.Copy(out, in); err != nil {
			out.Close()
			os.Remove(out.Name())
			return "", err
		}
		return out.Name(), out.Close()
	}
	return "", errors.New("flowlite.exe not found inside the release zip")
}

// sanityCheck runs the new binary once before it replaces anything, so a
// truncated or wrong-architecture file can never take the old one's place.
func sanityCheck(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return fmt.Errorf("the downloaded binary does not run (%v) — nothing was changed", err)
	}
	if !strings.HasPrefix(string(out), "flowlite ") {
		return fmt.Errorf("the downloaded file is not flowlite (%q) — nothing was changed", strings.TrimSpace(string(out)))
	}
	return nil
}

// replaceBinary moves the new file over target. On Unix a rename over a
// running executable is fine: the old inode lives on until the process
// exits. Windows locks a running .exe against replacement but does allow
// renaming it, so the old one is moved aside to .old first; if even that
// fails the new file is left beside it with instructions.
func replaceBinary(newPath, target string) error {
	if runtime.GOOS != "windows" {
		return os.Rename(newPath, target)
	}
	old := target + ".old"
	_ = os.Remove(old) // from a previous update
	if err := os.Rename(target, old); err == nil {
		if err := os.Rename(newPath, target); err == nil {
			_ = os.Remove(old) // fails while the old exe is still running; harmless
			return nil
		}
		_ = os.Rename(old, target) // put things back
	}
	pending := target + ".new"
	_ = os.Remove(pending)
	if err := os.Rename(newPath, pending); err != nil {
		return err
	}
	fmt.Println(warn("  Windows would not let the running flowlite.exe be replaced."))
	fmt.Println("  The new version is saved next to it. Close every flowlite window, then run:")
	fmt.Printf("    move /Y \"%s\" \"%s\"\n", pending, target)
	return errors.New("update downloaded but not yet applied")
}

// --- once-a-day notice -----------------------------------------------------

// checkCache is update-check.json in the config dir: when GitHub was last
// asked and what it said. Reading it is how `flowlite` avoids touching the
// network more than once a day.
type checkCache struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
}

func checkCachePath() (string, error) {
	d, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "update-check.json"), nil
}

func readCheckCache() (checkCache, error) {
	p, err := checkCachePath()
	if err != nil {
		return checkCache{}, err
	}
	return readCheckCacheFile(p)
}

func readCheckCacheFile(p string) (checkCache, error) {
	var c checkCache
	b, err := os.ReadFile(p)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal(b, &c)
	return c, err
}

func writeCheckCache(c checkCache) error {
	p, err := checkCachePath()
	if err != nil {
		return err
	}
	return writeCheckCacheFile(p, c)
}

func writeCheckCacheFile(p string, c checkCache) error {
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// latestForNotice returns the newest known tag, asking GitHub only when the
// cached answer is older than notifyEvery. Any failure yields "" — the
// notice is best-effort and must never slow down or break the command that
// hosts it.
func latestForNotice(now time.Time) string {
	c, err := readCheckCache()
	if err == nil && now.Sub(c.CheckedAt) < notifyEvery && c.Latest != "" {
		return c.Latest
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	rel, err := fetchLatest(ctx)
	if err != nil {
		// Remember the failed attempt too, so a machine without network is
		// not probed on every single invocation.
		_ = writeCheckCache(checkCache{CheckedAt: now, Latest: c.Latest})
		return c.Latest
	}
	_ = writeCheckCache(checkCache{CheckedAt: now, Latest: rel.TagName})
	return rel.TagName
}

// updateNotice returns one line — "v0.5.1 available — flowlite update" — when
// a newer release exists, and "" otherwise. It is best-effort: at most one
// network request a day (2 s timeout), never an error, silent in scripts
// (stdout must be a terminal), silent for unversioned builds, and silenced
// entirely by FLOWLITE_NO_UPDATE_CHECK. The root banner and doctor print it.
func updateNotice() string {
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return ""
	}
	latest, ok := latestKnown()
	if !ok || !isNewer(latest, Version) {
		return ""
	}
	return warn(latest+" available") + dim(" — flowlite update")
}

// updateStatus is the doctor header's wording for the same check: the
// notice, "up to date", or why nothing is known.
func updateStatus() string {
	if os.Getenv(noUpdateCheckEnv) != "" {
		return dim("check disabled (" + noUpdateCheckEnv + ")")
	}
	if _, ok := parseVersion(Version); !ok {
		return dim("not checked (unversioned build)")
	}
	latest, ok := latestKnown()
	switch {
	case !ok || latest == "":
		return dim("unknown — could not reach GitHub")
	case isNewer(latest, Version):
		return warn(latest+" available") + dim(" — flowlite update")
	}
	return dim("up to date")
}

// latestKnown is the newest release tag, from the cache or (once a day) from
// GitHub. ok is false when the check is disabled or this build has no
// comparable version.
func latestKnown() (latest string, ok bool) {
	if os.Getenv(noUpdateCheckEnv) != "" || os.Getenv("CI") != "" {
		return "", false
	}
	if _, ok := parseVersion(Version); !ok {
		return "", false
	}
	return latestForNotice(time.Now()), true
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheckOnly, "check", false, "only report whether a newer release exists")
	updateCmd.Flags().StringVar(&updateTo, "to", "", "replace this file instead of the running binary (for testing)")
	_ = updateCmd.Flags().MarkHidden("to")
	rootCmd.AddCommand(updateCmd)
}
