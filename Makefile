IMAGE_NAME=sidecar-dev
CONTAINER_NAME=sidecar-runner
GO_VERSION=1.23rc1
ENTRYPOINT=main.go
CONTEXT=.

.PHONY: all build run shell

all: build run

build:
	echo "FROM golang:$(GO_VERSION)" > Dockerfile.temp
	echo "RUN apt update" >> Dockerfile.temp
	echo "RUN apt install -y libsystemd-dev" >> Dockerfile.temp
	echo "WORKDIR /app" >> Dockerfile.temp
	docker build -t $(IMAGE_NAME) -f Dockerfile.temp .
	rm Dockerfile.temp

run:
ifndef CONTAINER_ID
	$(error Please provide CONTAINER_ID, e.g. make run CONTAINER_ID=a8fc3ade5a9c)
endif
	docker run --rm -it \
		-v "$(PWD)":/app \
		-w /app \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-p 8080:8080 \
		--name $(CONTAINER_NAME) \
		$(IMAGE_NAME) \
		bash -c "export GOTOOLCHAIN=auto && go run $(ENTRYPOINT) -docker-container-id $(CONTAINER_ID)"

shell:
	docker run --rm -it \
		-v "$(PWD)":/app \
		-w /app \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-p 8080:8080 \
		--name $(CONTAINER_NAME) \
		$(IMAGE_NAME) \
		bash
