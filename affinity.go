//go:build linux

package workerpool

import (
	"golang.org/x/sys/unix"
	"runtime"
)

func PinToCPU(cpu int) error {
	runtime.LockOSThread()

	var mask unix.CPUSet
	mask.Zero()
	mask.Set(cpu)

	return unix.SchedSetaffinity(0, &mask)
}
