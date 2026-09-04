// FlowLite: press a key, speak, and the words land where your cursor is.
// Everything runs on this machine.
package main

import (
	"os"

	"github.com/sanke08/flowlite/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
