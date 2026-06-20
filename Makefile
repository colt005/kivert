.PHONY: all build test fmt vet lint docker-build docker-push helm-lint run-local clean

IMAGE_REPO ?= ghcr.io/colt005/kivert
IMAGE_TAG ?= latest

all: build test

build: fmt vet
	mkdir -p bin
	go build -o bin/kivert cmd/manager/main.go

test:
	go test -v ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

lint: fmt vet
	@echo "Lint checks passed."

docker-build:
	docker build -t $(IMAGE_REPO):$(IMAGE_TAG) .

docker-push:
	docker push $(IMAGE_REPO):$(IMAGE_TAG)

helm-lint:
	helm lint charts/kivert

run-local: fmt vet
	@echo "Running Kivert locally against active context. Leader election disabled for local dev."
	go run cmd/manager/main.go --leader-election=false --watch-all-namespaces=true --log-level=debug

clean:
	rm -rf bin/
