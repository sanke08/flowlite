package permissions

// MicStatus is the state of the microphone permission.
type MicStatus int

const (
	MicUnknown    MicStatus = iota // never asked; the prompt has not been shown
	MicGranted                     // recording will hear the user
	MicDenied                      // the user said no; only they can undo it
	MicRestricted                  // policy forbids it (parental controls, MDM)
)

// OK reports whether recording will actually capture audio.
func (m MicStatus) OK() bool { return m == MicGranted }

func (m MicStatus) String() string {
	switch m {
	case MicGranted:
		return "granted"
	case MicDenied:
		return "denied"
	case MicRestricted:
		return "restricted"
	default:
		return "not requested"
	}
}

// MicHint is what to tell the user for a status that is not granted.
func (m MicStatus) Hint() string {
	switch m {
	case MicDenied:
		return "microphone access was denied — System Settings → Privacy & Security → Microphone, switch on your terminal (or FlowLite), then restart it"
	case MicRestricted:
		return "microphone access is blocked by a policy on this Mac (Screen Time or an MDM profile)"
	default:
		return "microphone access has not been granted yet — run `flowlite` once and allow it when macOS asks"
	}
}
