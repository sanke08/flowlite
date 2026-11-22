//go:build windows

package catalog

import (
	"errors"
	"syscall"
)

// tryLock opens path with no sharing at all, which is Windows' own exclusive
// lock: a second open fails with a sharing violation until the handle is
// closed, and the system closes it when the process exits, so a crashed
// download never leaves it stuck.
func tryLock(path string) (release func(), err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ|syscall.GENERIC_WRITE, 0, nil,
		syscall.OPEN_ALWAYS, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		const errorSharingViolation syscall.Errno = 32
		if errors.Is(err, errorSharingViolation) {
			return nil, ErrDownloadBusy
		}
		return nil, err
	}
	return func() { _ = syscall.CloseHandle(h) }, nil
}
