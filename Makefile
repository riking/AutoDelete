CONTAINER_NAME := "local/autodelete"

.PHONY: build
build:
	docker build -t $(CONTAINER_NAME) .

.PHONY: run
run:
	docker run --rm $(CONTAINER_NAME)
