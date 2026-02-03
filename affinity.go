//go:build linux

package workerpool

import (
	"golang.org/x/sys/unix"
)

// PinToCPU pins the current OS thread to a specific CPU.
//
// It restricts the calling thread to run only on the given CPU core.
// This is typically used in conjunction with runtime.LockOSThread
// to improve cache locality and reduce scheduler-induced migration.
//
// This function is Linux-specific and has no effect on other platforms.
func PinToCPU(cpu int) error {
	var mask unix.CPUSet
	mask.Zero()
	mask.Set(cpu)
	return unix.SchedSetaffinity(0, &mask)
}
