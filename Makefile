.PHONY: test fmt vet build run backends bench

test:
	go test -race -count=1 ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

build:
	go build -o bin/lb ./cmd/lb
	go build -o bin/backend ./examples/backend

# Three local backends; the third is slow. Stop them with Ctrl+C.
backends: build
	bin/backend -addr :9001 -name backend-1 & \
	bin/backend -addr :9002 -name backend-2 & \
	bin/backend -addr :9003 -name backend-3 -delay 200ms & \
	wait

run: build
	bin/lb -config configs/l7.json

bench:
	./scripts/bench.sh
