IMAGE_NAME=ghcr.io/fastlane-labs/fastlane-sidecar
CONTAINER_NAME=sidecar-runner
# Auto-detect version from git: use tag if on a tag, otherwise use commit SHA
VERSION?=$(shell git describe --tags --exact-match 2>/dev/null | sed 's/^v//' || echo "0~dev.$$(git rev-parse --short HEAD)")
IPC_PATH?=/home/monad/monad-bft/fastlane.sock

.PHONY: build run shell build-deb

build:
	docker build -t $(IMAGE_NAME) .

run:
	docker run --rm -it \
		-v $(dir $(IPC_PATH)):$(dir $(IPC_PATH)) \
		--name $(CONTAINER_NAME) \
		$(IMAGE_NAME) \
		-ipc-path=$(IPC_PATH)

shell:
	docker run --rm -it \
		-v $(dir $(IPC_PATH)):$(dir $(IPC_PATH)) \
		--name $(CONTAINER_NAME) \
		--entrypoint /bin/sh \
		$(IMAGE_NAME)

build-deb:
	./debian/build-deb.sh $(VERSION)
