IMAGE_NAME=screenshot
CONTAINER_NAME=screenshot-container
PORT=80

build:
	docker build -t $(IMAGE_NAME) .

run: build
	docker run --rm -d -p $(PORT):80 --name $(CONTAINER_NAME) $(IMAGE_NAME)

stop:
	docker stop $(CONTAINER_NAME) || true
	docker rm $(CONTAINER_NAME) || true

restart: stop run

logs:
	docker logs -f $(CONTAINER_NAME)
