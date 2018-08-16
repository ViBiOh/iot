APP_NAME ?= iot
VERSION ?= $(shell git log --pretty=format:'%h' -n 1)
AUTHOR ?= $(shell git log --pretty=format:'%an' -n 1)

default:
	docker build -t vibioh/$(APP_NAME):$(VERSION) .

$(APP_NAME): deps go

go: format lint tst bench build

name:
	@echo -n $(APP_NAME)

version:
	@echo -n $(VERSION)

author:
	@python -c 'import sys; import urllib; sys.stdout.write(urllib.quote_plus(sys.argv[1]))' "$(AUTHOR)"

deps:
	go get github.com/golang/dep/cmd/dep
	go get github.com/golang/lint/golint
	go get github.com/kisielk/errcheck
	go get golang.org/x/tools/cmd/goimports
	dep ensure

format:
	goimports -w */*/*.go
	gofmt -s -w */*/*.go

lint:
	golint `go list ./... | grep -v vendor`
	errcheck -ignoretests `go list ./... | grep -v vendor`
	go vet ./...

tst:
	script/coverage

bench:
	go test ./... -bench . -benchmem -run Benchmark.*

build-api:
	CGO_ENABLED=0 go build -ldflags="-s -w" -installsuffix nocgo -o bin/iot cmd/api/iot.go
	CGO_ENABLED=0 go build -ldflags="-s -w" -installsuffix nocgo -o bin/worker cmd/worker/worker.go

start-deps:
	go get github.com/ViBiOh/auth/cmd/bcrypt

start-worker:
	go run cmd/worker/worker.go \
		-websocket ws://localhost:1080/ws/hue \
		-secretKey SECRET_KEY \
		-hueConfig ./hue.json \
		-hueUsername $(BRIDGE_USERNAME) \
		-hueBridgeIP $(BRIDGE_IP) \
		-hueClean

start:
	go run cmd/api/iot.go \
		-tls=false \
		-authUsers admin:admin \
		-basicUsers "1:admin:`bcrypt admin`" \
		-secretKey SECRET_KEY \
		-csp "default-src 'self'; style-src 'unsafe-inline'"

.PHONY: $(APP_NAME) go name version author deps format lint tst bench build start-deps start-worker start
