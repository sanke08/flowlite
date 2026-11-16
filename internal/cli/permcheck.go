package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sanke08/flowlite/internal/permissions"
)

// runPermCheck is the hidden `--perm-check FILE` plumbing: report this
// process's own Accessibility and microphone state into FILE. ownPermissions
// runs it under launchd so the answer is flowlite's, not the terminal's.
func runPermCheck(out string, request bool) error {
	if request {
		if !permissions.Trusted() {
			permissions.Request() // adds flowlite to the Accessibility list
		}
		if permissions.Mic() == permissions.MicUnknown {
			permissions.RequestMic()
		}
	}
	s := fmt.Sprintf("ax=%t\nmic=%d\n", permissions.Trusted(), int(permissions.Mic()))
	tmp := out + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, out) // the reader never sees half a file
}

func parsePermCheck(s string) (ax bool, mic permissions.MicStatus, ok bool) {
	var haveAX, haveMic bool
	for _, line := range strings.Split(s, "\n") {
		k, v, found := strings.Cut(strings.TrimSpace(line), "=")
		if !found {
			continue
		}
		switch k {
		case "ax":
			ax, haveAX = v == "true", true
		case "mic":
			n, err := strconv.Atoi(v)
			if err == nil {
				mic, haveMic = permissions.MicStatus(n), true
			}
		}
	}
	return ax, mic, haveAX && haveMic
}
