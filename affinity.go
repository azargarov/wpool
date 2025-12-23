//go:build linux
package workerpool

import (
    "runtime"
    "golang.org/x/sys/unix"
)

func PinToCPU(cpu int) error {
    runtime.LockOSThread()

    var mask unix.CPUSet
    mask.Zero()
    mask.Set(cpu)

    return unix.SchedSetaffinity(0, &mask)
}
