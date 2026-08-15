# TODO: NEED TO CLEAN UP THE ENV STUFF, AND PROBABLY ABANDON INIT.SH
all: build test

build:
	clear
	go build -o main cmd/api/main.go

docker-build: test
	docker build -t gift-registry -f Dockerfile .
	docker tag gift-registry container-registry.hydrick-local.net/gift-registry:latest
	docker push container-registry.hydrick-local.net/gift-registry:latest

env-local: 
	clear
	./init.sh --local -e .env_local

env-test: 
	clear
	./init.sh --test 

env-prod: 
	clear
	./init.sh --prod 

fmt:
	clear
	go fmt ./...

install: 
	go install honnef.co/go/tools/cmd/staticcheck@latest

local-down:
	clear
	docker compose --env-file=.env_local -f docker-compose.yml down

local-up: test
	docker compose --env-file=.env_local -f docker-compose.yml up -d --no-deps
	docker ps -a

prod-down:
	clear
	docker compose -f docker-compose-prod.yml down

prod-up: docker-build
	docker compose --env-file=.env_prod -f docker-compose-prod.yml up -d --no-deps
	docker ps -a

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
