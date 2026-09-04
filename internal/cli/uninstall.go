package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sanke08/flowlite/internal/catalog"
	"github.com/sanke08/flowlite/internal/config"
)

var uninstallYes bool

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove FlowLite completely: models, settings and the binary",
	RunE: func(cmd *cobra.Command, args []string) error {
		dir, err := config.Dir()
		if err != nil {
			return err
		}
		var models int64
		for _, m := range catalog.Installed() {
			models += m.DiskBytes()
		}
		exe, _ := os.Executable()
		exe, _ = filepath.EvalSymlinks(exe)

		fmt.Println(bold("This will remove:"))
		fmt.Printf("  %s   (settings, log, models — %s)\n", shortenHome(dir), catalog.Human(models))
		fmt.Printf("  %s\n", exe)
		if !uninstallYes {
			fmt.Print("\nType yes to continue: ")
			var answer string
			fmt.Scanln(&answer)
			if answer != "yes" {
				fmt.Println("cancelled")
				return nil
			}
		}
		if pid, running := daemonRunning(); running {
			_ = terminate(pid)
		}
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
		// Removing a running executable is fine on macOS/Linux: the file is
		// unlinked and this process keeps running to the end.
		if err := os.Remove(exe); err != nil {
			fmt.Println(warn("  could not remove " + exe + ": " + err.Error()))
			fmt.Println(dim("  remove it yourself, e.g.: sudo rm " + exe))
		}
		_ = exec.Command("true").Run()
		fmt.Printf("%s FlowLite removed. Thanks for trying it.\n", ok("✓"))
		return nil
	},
}

func init() {
	uninstallCmd.Flags().BoolVarP(&uninstallYes, "yes", "y", false, "do not ask for confirmation")
	rootCmd.AddCommand(uninstallCmd)
}
