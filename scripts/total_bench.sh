#!/usr/bin/env bash
set -euo pipefail

RAW=bench.raw
OUT=bench.md

echo "Running benchmarks…"
go test -bench=. -benchmem -count=1 > "$RAW"


get_line() {
  grep -E "^$1" "$RAW" || true
}

extract_col() {
  local line="$1"
  local name="$2"
  awk -v key="$name" '
    {
      for (i=1; i<=NF; i++) {
        if ($i == key) print $(i-1)
      }
    }
  ' <<< "$line"
}

delta() {
  local v2="$1"
  local base="$2"

  if [[ -z "$v2" || -z "$base" ]]; then
    echo "—"
    return
  fi

  printf "%.0f%%" "$(echo "scale=6; ($v2 - $base) / $base * 100" | bc -l)"
}

submitters=($(grep -oE "BenchmarkSubmitPath/[^/]+/([0-9]+)_submitters" "$RAW" \
  | sed 's/.*\///;s/_submitters//' | sort -n | uniq))

queues=("Fifo" "BucketQueue" "Priority")

declare -A mjobs
declare -A ns
declare -A bytes
declare -A allocs

echo "Parsing SubmitPath results…"

for q in "${queues[@]}"; do
  for s in "${submitters[@]}"; do
    line=$(get_line "BenchmarkSubmitPath/$q/${s}_submitters")
    mjobs[$q,$s]=$(extract_col "$line" "Mjobs/sec")
    ns[$q,$s]=$(extract_col "$line" "ns/op")
    bytes[$q,$s]=$(extract_col "$line" "B/op")
    allocs[$q,$s]=$(extract_col "$line" "allocs/op")
  done
done


echo "Generating $OUT…"

{
echo "## Benchmark Results"
echo
echo "\`goos: $(go env GOOS) · goarch: $(go env GOARCH) · $(date +'%Y-%m-%d'),"
echo  "CPU: " `lscpu -p=MODELNAME| tail -n 1` "\`"

echo
echo "### Submit Performance — Mjobs/sec"
echo
echo "| Submitters | Fifo  | BucketQueue | Priority |"
echo "|------------|-------|-------------|----------|"

for s in "${submitters[@]}"; do
  printf "| %-10s | %-4s | %-11s | %-8s |\n" "$s"  ${mjobs[Fifo,$s]:-—} ${mjobs[BucketQueue,$s]:-—} ${mjobs[Priority,$s]:-—}
done

echo
echo "### Submit Performance — ns/op"
echo
echo "| Submitters | Fifo   | BucketQueue | Priority |"
echo "|------------|--------|-------------|----------|"

for s in "${submitters[@]}"; do
  printf "| %-10s | %-4s | %-11s | %-8s |\n" "$s"  ${ns[Fifo,$s]:-—} ${ns[BucketQueue,$s]:-—} ${ns[Priority,$s]:-—}
  #echo "| $s | ${ns[Fifo,$s]:-—} | ${ns[BucketQueue,$s]:-—} | ${ns[Priority,$s]:-—} |"
done

echo
echo "### Submit Performance — B/op"
echo
echo "| Submitters | Fifo | BucketQueue | Priority |"
echo "|------------|------|-------------|----------|"

for s in "${submitters[@]}"; do
  printf "| %-10s | %-4s | %-11s | %-8s |\n" "$s"  ${bytes[Fifo,$s]:-—} ${bytes[BucketQueue,$s]:-—} ${bytes[Priority,$s]:-—}
done

echo
echo "### Submit Performance — allocs/op"
echo
echo "| Submitters | Fifo | BucketQueue | Priority |"
echo "|------------|------|-------------|----------|"

for s in "${submitters[@]}"; do
  printf "| %-10s | %-4s | %-11s | %-8s |\n" $s  ${allocs[Fifo,$s]:-—} ${allocs[BucketQueue,$s]:-—} ${allocs[Priority,$s]:-—}
done
} > "$OUT"

# delete temp file
rm "$RAW"

