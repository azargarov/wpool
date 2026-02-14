package workerpool

import (
    "runtime"
)

type segmentPool[T any] struct {
    pageSize uint32

    maxKeep int
	free    []*segment[T]

    // fast paths
    putCh chan *segment[T]      
    getCh chan *segment[T]      

 	refillCh chan struct{}

    // tuning
    prefetch int 

	metrics *segmentPoolMetrics

    done chan struct{}
}

func (p *segmentPool[T]) adaptivePrefetch() int {
    capacity := cap(p.getCh)
    current := len(p.getCh)
    
    if current < capacity/4 {
        return p.prefetch
    }
    return p.prefetch >> 1
}

func NewSegmentPool[T any](pageSize uint32, prefill int, maxKeep int, fastPut int, fastGet int) *segmentPool[T] {
    if maxKeep <= 0 {
        maxKeep = max(prefill * 2, 128)
    }
    if prefill < 0 { prefill = 4 }
    if fastPut <= 0 { fastPut = 1024 }
    if fastGet <= 0 { fastGet = 1024 }

    p := &segmentPool[T]{
        pageSize:  pageSize,
        maxKeep:   maxKeep,
        done: make(chan struct{}),
        free:      make([]*segment[T], 0, maxKeep),
        putCh:     make(chan *segment[T], fastPut),
        getCh:     make(chan *segment[T], fastGet),

		refillCh:  make(chan struct{}, 1),
        prefetch:  256,
    }

    for range(prefill) {
        p.free = append(p.free, mkSegment[T](pageSize))
    }

	p.metrics = &segmentPoolMetrics{}

    go p.run()
    return p
}

func (p *segmentPool[T]) Put(seg *segment[T]) {

    if seg == nil { return }

    select {
    case p.getCh <- seg:
        p.metrics.IncFastPutHit()
        return
    default:
        p.metrics.IncFastPutDrop()
    }

    select {
    case p.putCh <- seg:
        p.metrics.IncFastPutHit()
    default:
    }
}

func (p *segmentPool[T]) Get() *segment[T] {
    select {
    case seg := <-p.getCh:
        p.metrics.IncFastGetHit()
        return seg
    default:
        p.metrics.IncFastGetMiss()
    }

    select {
    case p.refillCh <- struct{}{}:
        p.metrics.IncRefillSignal()
    default:
    }

    for range 8 {
        select {
        case seg := <-p.getCh:
            p.metrics.IncFastGetHit() 
            return seg
        default:
            runtime.Gosched()
        }
    }

    p.metrics.IncFallbackAlloc()
    return mkSegment[T](p.pageSize)
}

func (p *segmentPool[T]) run() {

    refill := func() {
		p.metrics.IncRefillCall()
        for i := 0; i < p.adaptivePrefetch(); i++ {

            var seg *segment[T]

            if len(p.free) > 0 {
                n := len(p.free) - 1
                seg = p.free[n]
                p.free[n] = nil
                p.free = p.free[:n]

				p.metrics.IncRefillFromFree()
            } else {
                seg = mkSegment[T](p.pageSize)
				p.metrics.IncRefillAllocated()
            }

            select {
            case p.getCh <- seg:
            case <-p.done:
                return
            default:
                if len(p.free) < p.maxKeep {
                    p.free = append(p.free, seg)
                }
                return
            }
        }
    }
    refill()
    for {
        select {
        case <-p.done:
            return
        case seg := <-p.putCh:
            if len(p.free) < p.maxKeep {
                p.free = append(p.free, seg)
            }

        case <-p.refillCh:
            refill()
        }
    }
}

func (p *segmentPool[T]) StatSnapshot() string{
	return p.metrics.Snapshot().String()
}

func (p *segmentPool[T]) Close() {
    close(p.done)
}