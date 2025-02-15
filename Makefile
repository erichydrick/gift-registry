all: build test

build: 
	go build -o main cmd/api/main.go

docker-build: build
	docker build -t gift-registry -f docker/Dockerfile .

docker-compose-down: 
	docker compose -f docker/docker-compose.yml down

docker-compose-up: #docker-build
	docker compose -f docker/docker-compose.yml up -d
	docker ps -a

test: 
	echo "RUN THE TESTS!"
