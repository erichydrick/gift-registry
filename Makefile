all: build test

build: 
	echo "BUILD THE APPLICATION"

docker-build: build
	docker build -t gift-registry -f docker/Dockerfile .

docker-compose-down: 
	docker compose -f docker/docker-compose.yml down

docker-compose-up: #docker-build
	docker compose -f docker/docker-compose.yml up -d

test: 
	echo "RUN THE TESTS!"
