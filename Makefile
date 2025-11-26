.PHONY: lint test bench

lint:
	go fmt ./...
	go vet ./...
	golangci-lint run ./...

test:
	go test ./...

bench:
	go test -bench=BenchmarkSubmitPath -gcflags="all=-l=4 -B" -benchmem
	#go test -bench=BenchmarkSubmitPath -gcflags="-m -m" 2>&1 | grep BenchmarkSubmitPath
	#go test -bench=. 