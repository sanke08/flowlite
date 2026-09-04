package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Copy this binary onto your PATH so `flowlite` works from anywhere",
	RunE: func(cmd *cobra.Command, args []string) error {
		dest, err := installSelf()
		if err != nil {
			return err
		}
		fmt.Printf("%s installed to %s\n", ok("✓"), dest)
		if !onPath(filepath.Dir(dest)) {
			fmt.Println(warn("  " + filepath.Dir(dest) + " is not on your PATH yet."))
			fmt.Println(dim("  add this line to ~/.zshrc, then open a new terminal:"))
			fmt.Printf("    export PATH=\"%s:$PATH\"\n", filepath.Dir(dest))
		}
		return nil
	},
}

// installDir picks the first writable, conventional bin directory.
func installDir() string {
	for _, d := range []string{"/opt/homebrew/bin", "/usr/local/bin"} {
		if f, err := os.CreateTemp(d, ".flowlite-probe"); err == nil {
			f.Close()
			os.Remove(f.Name())
			return d
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

// installSelf copies the running executable into installDir.
func installSelf() (string, error) {
	src, err := os.Executable()
	if err != nil {
		return "", err
	}
	src, _ = filepath.EvalSymlinks(src)
	dir := installDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dest := filepath.Join(dir, "flowlite")
	if runtime.GOOS == "windows" {
		dest += ".exe"
	}
	if same, _ := sameFile(src, dest); same {
		return dest, nil
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	out.Close()
	if err := os.Rename(tmp, dest); err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		// A binary that arrived by browser download carries a quarantine flag
		// that Gatekeeper enforces on every launch; the copy should not.
		_ = exec.Command("xattr", "-d", "com.apple.quarantine", dest).Run()
	}
	return dest, nil
}

func sameFile(a, b string) (bool, error) {
	fa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	fb, err := os.Stat(b)
	if err != nil {
		return false, nil
	}
	return os.SameFile(fa, fb), nil
}

func onPath(dir string) bool {
	for _, p := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if p == dir {
			return true
		}
	}
	return false
}

// runningFromPath reports whether the current executable lives on PATH.
func runningFromPath() bool {
	exe, err := os.Executable()
	if err != nil {
		return true
	}
	exe, _ = filepath.EvalSymlinks(exe)
	return onPath(filepath.Dir(exe))
}


func init() {
	rootCmd.AddCommand(installCmd)
}
