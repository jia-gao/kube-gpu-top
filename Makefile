BINARY_CLI = kubectl-gpu_top
BINARY_AGENT = kube-gpu-agent
IMAGE_REPO = ghcr.io/jia-gao/kube-gpu-agent
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: all proto build build-cli build-agent test lint clean docker-build docker-push

all: proto build

proto:
	protoc --go_out=api/gpuagent --go_opt=paths=source_relative \
		--go-grpc_out=api/gpuagent --go-grpc_opt=paths=source_relative \
		-I api/proto api/proto/gpuagent.proto

build: build-cli build-agent

build-cli:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" \
		-o bin/$(BINARY_CLI) ./cmd/kubectl-gpu-top/

build-agent:
	CGO_ENABLED=0 go build -ldflags="-s -w -X main.version=$(VERSION)" \
		-o bin/$(BINARY_AGENT) ./cmd/kube-gpu-agent/

test:
	go test ./... -v

lint:
	golangci-lint run

clean:
	rm -rf bin/

docker-build:
	docker build -t $(IMAGE_REPO):$(VERSION) -f Dockerfile.agent .

docker-push: docker-build
	docker push $(IMAGE_REPO):$(VERSION)
