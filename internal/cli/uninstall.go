package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove FlowLite completely: models, settings, history, log and the binary",
	Args:  cobra.NoArgs,
	RunE:  runUninstall,
}

func runUninstall(cmd *cobra.Command, args []string) error {
	_, err := uninstallFlowLite()
	return err
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

// uninstallFlowLite removes everything: models, settings, history, log and
// the binary itself. It asks the user to type "yes" first. Returns true when
// FlowLite is gone, so the caller can stop offering a menu for a program
// that no longer exists.
func uninstallFlowLite() (bool, error) {
	dir, err := config.Dir()
	if err != nil {
		return false, err
	}
	var models int64
	for _, m := range catalog.Installed() {
		models += m.DiskBytes()
	}
	exe, _ := os.Executable()
	exe, _ = filepath.EvalSymlinks(exe)

	fmt.Println(bold("This will remove:"))
	fmt.Printf("  %s   (settings, log, history, models — %s)\n", shortenHome(dir), catalog.Human(models))
	fmt.Printf("  %s\n", exe)
	fmt.Print("\nType yes to continue: ")
	var answer string
	fmt.Scanln(&answer)
	if answer != "yes" {
		fmt.Println("cancelled")
		return false, nil
	}
	if pid, running := daemonRunning(); running {
		_ = terminate(pid)
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	// Removing a running executable is fine on macOS/Linux: the file is
	// unlinked and this process keeps running to the end.
	if err := os.Remove(exe); err != nil {
		fmt.Println(warn("  could not remove " + exe + ": " + err.Error()))
		fmt.Println(dim("  remove it yourself, e.g.: sudo rm " + exe))
	}
	fmt.Printf("%s FlowLite removed. Thanks for trying it.\n", ok("✓"))
	return true, nil
}
