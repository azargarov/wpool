//go:build linux

package workerpool

import (
	"golang.org/x/sys/unix"
)

func PinToCPU(cpu int) error {
	var mask unix.CPUSet
	mask.Zero()
	mask.Set(cpu)
	return unix.SchedSetaffinity(0, &mask)
}
