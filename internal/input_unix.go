//go:build !windows

package internal

import (
	"errors"
	"time"

	"golang.org/x/sys/unix"
)

func waitForInputPlatform(fd int, timeout time.Duration) (bool, error) {
	if timeout <= 0 {
		timeout = 10 * time.Millisecond
	}

	pollTimeout := int(timeout / time.Millisecond)
	if pollTimeout <= 0 {
		pollTimeout = 1
	}

	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}

	n, err := unix.Poll(fds, pollTimeout)
	if err != nil {
		if errors.Is(err, unix.EINTR) {
			return false, nil
		}
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	return fds[0].Revents&unix.POLLIN != 0, nil
}
