#!/bin/env bash

# 1. Free-scheduled — let OS place goroutines
go test -run=^$ -bench='^BenchmarkPool_Latency$' -benchmem -count=3

# 2. Segment size test

for seg in 1 4 16 64 256 1024 4096; do
    echo "=== SEGSIZE=$seg ==="
    SEGSIZE=$seg SEGCOUNT=128 \
        go test ./... -run=^$ \
        -bench='BenchmarkPool_Single/empty_' \
        -benchmem -count=3
done

# 3. NUMA-local — force everything to socket 0
if  command -v numactl >/dev/null 2>&1; then
    numactl --cpunodebind=0 --membind=0 \
      go test -run=^$ -bench='^BenchmarkPool_Latency$' -benchmem -count=3
    node_count=$(numactl -H | grep "^available:" | awk '{print $2}')
    if [ "${node_count}" -gt 1 ]; then
        
        numactl --cpunodebind=1 --membind=0 \
          go test -run=^$ -bench='^BenchmarkPool_Latency$' -benchmem -count=3        
        
        numactl --cpunodebind=1 --membind=1 \
          go test -run=^$ -bench='^BenchmarkPool_Latency$' -benchmem -count=3
    fi
else
    echo "numactl not found"
fi