SHELL := /bin/bash

NAME=screenshot
PORT=80

dev:
	@docker build -f Dockerfile.dev -t $(NAME) .
	@docker run --rm -it -p $(PORT):80 -v $(PWD):/app $(NAME)

up:
	@make dev

test:
	@go test -v ./...

format:
	@go fmt ./...

push: format test
	@git add .
	@curl -s https://commit.jaw.dev/ | sh -s --  --no-verify -ai openai

clean:
	@docker ps -a --filter "ancestor=$(NAME)" -q | xargs -r docker stop || true
	@docker ps -a --filter "ancestor=$(NAME)" -q | xargs -r docker rm || true
	@docker rmi $(NAME) || true
	@docker builder prune -af
	@docker volume prune -f
	@rm -f *.db *.sqlite *.sqlite-shm *.sqlite-wal
	@rm -rf tmp logs

filters:
	@go run filter_parser.go

deploy:
	@set -a && source .env && set +a && npx caprover deploy \
		--caproverUrl "$$CAPROVER_DOMAIN" \
		--appToken "$$CAPROVER_APP_TOKEN" \
		--appName "$$CAPROVER_APP_NAME" \
		-b "$$(git rev-parse --abbrev-ref HEAD)"
