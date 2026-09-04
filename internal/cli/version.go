package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// Set by the Makefile via -ldflags -X.
var (
	Commit         = "unknown"
	BuildDate      = "unknown"
	WhisperVersion = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version and build details",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("flowlite %s\n", Version)
		fmt.Printf("  commit       %s\n", Commit)
		fmt.Printf("  built        %s\n", BuildDate)
		fmt.Printf("  whisper.cpp  %s\n", WhisperVersion)
		fmt.Printf("  go           %s  %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
