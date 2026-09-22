//go:build windows

package internal

import (
	"time"

	"golang.org/x/sys/windows"
)

func waitForInputPlatform(fd int, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = 10 * time.Millisecond
	}

	handle := windows.Handle(fd)
	waitMilliseconds := uint32(timeout / time.Millisecond)
	if waitMilliseconds == 0 {
		waitMilliseconds = 1
	}

	result, err := windows.WaitForSingleObject(handle, waitMilliseconds)
	if err != nil {
		return false, err
	}

	switch result {
	case uint32(windows.WAIT_OBJECT_0):
		return true, nil
	case uint32(windows.WAIT_TIMEOUT):
		return false, nil
	default:
		return false, nil
	}
}
