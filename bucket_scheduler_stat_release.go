//go:build !debug

package workerpool

func schedDdgIncBucketPush(i uint8 ){}
func schedDbgIncTotalPush(){}
func schedDbgIncPops(i uint8){}
func schedDbgAddTotalPops(n uint64){}
func schedDbgIncPopMisses(i uint8){}
func schedDbgIncRotateAborts(){}
func schedDbgIncMaskClears(){}
func schedDbgIncRotateMoves(){}
func schedDbgIncRotateCalls(){}
func ShedDumpStats() {}
