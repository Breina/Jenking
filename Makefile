.PHONY: build test test-e2e clean

build:
	go build ./cmd/jenking

test:
	go test ./...

test-e2e:
	go test -tags=integration -race ./test/e2e/scenarios/...

test-e2e-verbose:
	go test -tags=integration -race -v ./test/e2e/scenarios/...

probe:
	go run -tags=integration ./test/e2e/cmd/jenking-probe/

clean:
	rm -f jenking
