package workerpool

import (
    "runtime"
    "sync"
)

type segmentPool[T any] struct {
    pageSize uint32
    maxKeep  int

    hot  []*segment[T]      // Lock-free fast path via channels
    cold []*segment[T]      // Mutex-protected overflow
    mu   sync.Mutex

    // Fast paths
    putCh    chan *segment[T]
    getCh    chan *segment[T]
    refillCh chan struct{}

    prefetch int
    metrics  *segmentPoolMetrics
    done     chan struct{}
}

func NewSegmentPool[T any](pageSize uint32, prefill int, maxKeep int, fastPut int, fastGet int) *segmentPool[T] {
    if maxKeep <= 0 {
        maxKeep = max(prefill*2, 128)
    }
    if prefill < 0 {
        prefill = 4
    }
    if fastPut <= 0 {
        fastPut = 1024
    }
    if fastGet <= 0 {
        fastGet = 1024
    }

    p := &segmentPool[T]{
        pageSize: pageSize,
        maxKeep:  maxKeep,
        done:     make(chan struct{}),
        hot:      make([]*segment[T], 0, prefill),
        cold:     make([]*segment[T], 0, maxKeep),
        putCh:    make(chan *segment[T], fastPut),
        getCh:    make(chan *segment[T], fastGet),
        refillCh: make(chan struct{}, 1),
        prefetch: 8,
        metrics:  &segmentPoolMetrics{},
    }

    // Prefill hot storage
    for range prefill {
        p.hot = append(p.hot, mkSegment[T](pageSize))
    }

    go p.run()
    return p
}

func (p *segmentPool[T]) Put(seg *segment[T]) {
    if seg == nil {
        return
    }
    // fast path 
    select {
    case p.getCh <- seg:
        p.metrics.IncFastPutHit()
        return
    default:
    }

    // buffered putCh
    select {
    case p.putCh <- seg:
        p.metrics.IncFastPutToBuf()
        return
    default:
    }

    // mutex-protected cold storage 
    p.mu.Lock()
    if len(p.cold) < p.maxKeep {
        p.cold = append(p.cold, seg)
        p.mu.Unlock()
        p.metrics.IncFastPutToBuf()
        
        select {
        case p.refillCh <- struct{}{}:
        default:
        }
    } else {
        p.mu.Unlock()
        p.metrics.IncFastPutDrop()
    }
}

func (p *segmentPool[T]) Get() *segment[T] {
    // fast path
    select {
    case seg := <-p.getCh:
        if seg.refs.Load() != 0 {
            panic("segment from pool has non-zero refs")
        }
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
            if seg.refs.Load() != 0 {
                panic("segment from pool has non-zero refs")
            }
            p.metrics.IncFastGetHit()
            return seg
        default:
            runtime.Gosched()
        }
    }

    p.metrics.IncFallbackAlloc()
    return mkSegment[T](p.pageSize)
}

func (p *segmentPool[T]) adaptivePrefetch() int {
    capacity := cap(p.getCh)
    current := len(p.getCh)

    if current < (capacity >> 2) {
        return p.prefetch
    }
    return p.prefetch >> 1
}

func (p *segmentPool[T]) run() {
    refill := func() {
        p.metrics.IncRefillCall()

        // drain cold storage first 
        p.mu.Lock()
        coldBatch := p.cold
        p.cold = p.cold[:0] 
        p.mu.Unlock()

        for _, seg := range coldBatch {
            select {
            case p.getCh <- seg:
                p.metrics.IncRefillFromFree()
            case <-p.done:
                return
            default:
                p.mu.Lock()
                if len(p.hot) < p.maxKeep {
                    p.hot = append(p.hot, seg)
                }
                p.mu.Unlock()
                return
            }
        }

        // hot storage
        for range p.adaptivePrefetch() {
            var seg *segment[T]

            p.mu.Lock()
            if len(p.hot) > 0 {
                n := len(p.hot) - 1
                seg = p.hot[n]
                p.hot[n] = nil
                p.hot = p.hot[:n]
                p.mu.Unlock()
                p.metrics.IncRefillFromFree()
            } else {
                p.mu.Unlock()
                seg = mkSegment[T](p.pageSize)
                p.metrics.IncRefillAllocated()
            }

            select {
            case p.getCh <- seg:
            case <-p.done:
                return
            default:
                // getCh full, save for later
                p.mu.Lock()
                if len(p.hot) < p.maxKeep {
                    p.hot = append(p.hot, seg)
                }
                p.mu.Unlock()
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
            p.mu.Lock()
            if len(p.hot) < p.maxKeep {
                p.hot = append(p.hot, seg)
            }
            p.mu.Unlock()

        case <-p.refillCh:
            refill()
        }
    }
}

func (p *segmentPool[T]) StatSnapshot() string {
    return p.metrics.Snapshot().String()
}

func (p *segmentPool[T]) Close() {
    close(p.done)
}