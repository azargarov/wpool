## Benchmark Results

`goos: linux · goarch: amd64 · 2025-11-29,
CPU:  AMD Ryzen 7 8845HS w/ Radeon 780M Graphics `

### Submit Performance — Mjobs/sec

| Submitters | Fifo  | BucketQueue | Priority |
|------------|-------|-------------|----------|
| 1          | 14.06 | 14.96       | 16.11    |
| 2          | 12.45 | 11.84       | 12.21    |
| 32         | 7.719 | 7.696       | 7.668    |
| 64         | 7.515 | 7.600       | 7.739    |
| 128        | 7.480 | 7.604       | 7.599    |

### Submit Performance — ns/op

| Submitters | Fifo   | BucketQueue | Priority |
|------------|--------|-------------|----------|
| 1          | 0.1422 | 0.1337      | 0.1242   |
| 2          | 0.1606 | 0.1689      | 0.1638   |
| 32         | 0.2591 | 0.2599      | 0.2608   |
| 64         | 0.2661 | 0.2632      | 0.2584   |
| 128        | 0.2674 | 0.2630      | 0.2632   |

### Submit Performance — B/op

| Submitters | Fifo | BucketQueue | Priority |
|------------|------|-------------|----------|
| 1          | 0    | 0           | 0        |
| 2          | 0    | 0           | 0        |
| 32         | 0    | 0           | 0        |
| 64         | 0    | 0           | 0        |
| 128        | 0    | 0           | 0        |

### Submit Performance — allocs/op

| Submitters | Fifo | BucketQueue | Priority |
|------------|------|-------------|----------|
| 1          | 0    | 0           | 0        |
| 2          | 0    | 0           | 0        |
| 32         | 0    | 0           | 0        |
| 64         | 0    | 0           | 0        |
| 128        | 0    | 0           | 0        |
