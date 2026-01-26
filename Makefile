all: build test

build:
	clear
	go build -o main cmd/api/main.go

docker-build: test
	docker build -t gift-registry -f Dockerfile .

local-down:
	clear
	docker compose -f docker-compose-local.yml down

local-up: 
	docker compose --env-file=.env_local -f docker-compose-local.yml up -d --no-deps
	docker ps -a

fmt:
	clear
	go fmt ./...

prod-down:
	clear
	docker compose -f docker-compose-prod.yml down

prod-up: docker-build
	docker compose --env-file=.env_prod -f docker-compose-prod.yml up -d --no-deps
	docker ps -a

init: 
	clear
	./init.sh

staticcheck: fmt
	staticcheck ./...

test: staticcheck 
	go test ./...

test-down:
	clear
	docker compose -f docker-compose-test.yml down

test-up: docker-build
	docker compose --env-file=.env_test -f docker-compose-test.yml up -d --no-deps
	docker ps -a
