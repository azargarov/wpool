//go:build !debug

package workerpool

func statAllocated()  {}
func statRecycled()   {}
func statConsumed()   {}
func statCASMiss()    {}
func PrintStat() 	  {}
