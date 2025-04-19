all: build test

build:
	clear
	go build -o main cmd/api/main.go

docker-build: test
	docker build -t gift-registry -f docker/Dockerfile .

docker-down:
	clear
	docker compose -f docker/docker-compose.yml down
	docker system prune --volumes -f

docker-up: docker-build
	docker compose -f docker/docker-compose.yml up -d
	docker ps -a

fmt:
	clear
	go fmt ./...

init: 
	go install honnef.co/go/tools/cmd/staticcheck@latest

staticcheck: fmt
	staticcheck ./...

test: staticcheck 
	go test ./...
