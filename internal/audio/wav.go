package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// ReadWAV loads a 16-bit PCM WAV and returns mono float32 samples plus the
// file's sample rate. Stereo is averaged to mono. It exists for
// `flowlite test --file` and for tests; the live path never touches WAV.
func ReadWAV(path string) ([]float32, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return nil, 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return nil, 0, errors.New("not a RIFF/WAVE file")
	}

	var (
		channels, bits int
		rate           int
		data           []byte
	)
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			if err == io.EOF {
				break
			}
			return nil, 0, err
		}
		id := string(hdr[0:4])
		size := int(binary.LittleEndian.Uint32(hdr[4:8]))
		switch id {
		case "fmt ":
			buf := make([]byte, size)
			if _, err := io.ReadFull(f, buf); err != nil {
				return nil, 0, err
			}
			if binary.LittleEndian.Uint16(buf[0:2]) != 1 {
				return nil, 0, errors.New("only PCM WAV is supported")
			}
			channels = int(binary.LittleEndian.Uint16(buf[2:4]))
			rate = int(binary.LittleEndian.Uint32(buf[4:8]))
			bits = int(binary.LittleEndian.Uint16(buf[14:16]))
		case "data":
			data = make([]byte, size)
			if _, err := io.ReadFull(f, data); err != nil {
				return nil, 0, err
			}
		default:
			if _, err := f.Seek(int64(size+size%2), io.SeekCurrent); err != nil {
				return nil, 0, err
			}
		}
		if data != nil && rate != 0 {
			break
		}
	}
	if bits != 16 {
		return nil, 0, fmt.Errorf("only 16-bit WAV is supported (got %d)", bits)
	}
	if channels < 1 || data == nil {
		return nil, 0, errors.New("malformed WAV")
	}

	frames := len(data) / (2 * channels)
	out := make([]float32, frames)
	for i := 0; i < frames; i++ {
		var sum float32
		for c := 0; c < channels; c++ {
			s := int16(binary.LittleEndian.Uint16(data[(i*channels+c)*2:]))
			sum += float32(s) / 32768
		}
		out[i] = sum / float32(channels)
	}
	return out, rate, nil
}
