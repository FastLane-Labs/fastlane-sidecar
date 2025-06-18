IMAGE_NAME=ghcr.io/fastlane-labs/fastlane-sidecar
CONTAINER_NAME=sidecar-runner

.PHONY: run shell

run:
ifndef CONTAINER_ID
	$(error Please provide CONTAINER_ID, e.g. make run CONTAINER_ID=a8fc3ade5a9c)
endif
	docker run --rm -it \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-p 8080:8080 \
		--name $(CONTAINER_NAME) \
		$(IMAGE_NAME) \
		-docker-container-id $(CONTAINER_ID)

shell:
	docker run --rm -it \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-p 8080:8080 \
		--name $(CONTAINER_NAME) \
		$(IMAGE_NAME) \
		bash
