//go:build debug

package workerpool

import (
	"sync/atomic"
	"fmt"
)

type bucketDebug struct {
    pushes        atomic.Uint64
    pops          atomic.Uint64
    popMisses     atomic.Uint64
}

type schedulerDebug struct {
	totalPushes   atomic.Uint64
	totalPops     atomic.Uint64
    rotateCalls   atomic.Uint64
    rotateMoves   atomic.Uint64
    rotateAborts  atomic.Uint64
    maskClears    atomic.Uint64
}

var schedDbg struct {
    buckets       [BucketCount]bucketDebug
    sched         schedulerDebug
    base, mask    uint64
    hasWork       bool
}

func schedDdgIncBucketPush(i uint8 ){
	schedDbg.buckets[i].pushes.Add(1)
}
func schedDbgIncTotalPush(){
	schedDbg.sched.totalPushes.Add(1)
}
func schedDbgIncPops(i uint8){
	schedDbg.buckets[i].pops.Add(1)
}
func schedDbgAddTotalPops(n uint64){
	schedDbg.sched.totalPops.Add(n)
}
func schedDbgIncPopMisses(i uint8){
	schedDbg.buckets[i].popMisses.Add(1)
}
func schedDbgIncRotateAborts(){
	schedDbg.sched.rotateAborts.Add(1)
}
func schedDbgIncMaskClears(){
	schedDbg.sched.maskClears.Add(1)
}
func schedDbgIncRotateMoves(){
	schedDbg.sched.rotateMoves.Add(1)
}
func schedDbgIncRotateCalls(){
	schedDbg.sched.rotateCalls.Add(1)
}
func  ShedDumpStats(){ //base, mask uint64) {

    fmt.Printf(
        "rotates: calls=%d moves=%d aborts=%d clears=%d totalPushes=%d totalPops=%d\n",
        schedDbg.sched.rotateCalls.Load(),
        schedDbg.sched.rotateMoves.Load(),
        schedDbg.sched.rotateAborts.Load(),
        schedDbg.sched.maskClears.Load(),
		schedDbg.sched.totalPushes.Load(),
		schedDbg.sched.totalPops.Load(),
    )

    for i := range BucketCount {
        p := schedDbg.buckets[i].pushes.Load()
        c := schedDbg.buckets[i].pops.Load()
        m := schedDbg.buckets[i].popMisses.Load()
        if p|c|m != 0 {
            fmt.Printf(
                "  bucket[%02d]: push=%d pop=%d miss=%d\n",
                i, p, c, m,
            )
        }
    }
}
