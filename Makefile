all: build test

build:
	clear
	go build -o main cmd/api/main.go

docker-build: test
	docker build -t gift-registry -f docker/Dockerfile .

local-down:
	clear
	docker compose -f docker/docker-compose-local.yml down

local-up: docker-build
	docker compose --env-file=docker/.env_local -f docker/docker-compose-local.yml up -d --no-deps
	docker ps -a

fmt:
	clear
	go fmt ./...

prod-down:
	clear
	docker compose -f docker/docker-compose-prod.yml down

prod-up: docker-build
	docker compose --env-file=docker/.env_prod -f docker/docker-compose-prod.yml up -d --no-deps
	docker ps -a

init: 
	sudo apt-get install libavif16  
	go install honnef.co/go/tools/cmd/staticcheck@latest

staticcheck: fmt
	staticcheck ./...

test: staticcheck 
	go test ./...

test-down:
	clear
	docker compose -f docker/docker-compose-test.yml down

test-up: docker-build
	docker compose --env-file=docker/.env_test -f docker/docker-compose-test.yml up -d --no-deps
	docker ps -a
