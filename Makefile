# Auto-detect container runtime: prefer podman, fall back to docker.
# Override with: make DOCKER=docker
DOCKER ?= $(shell command -v podman >/dev/null 2>&1 && echo podman || echo docker)

# TODO: NEED TO CLEAN UP THE ENV STUFF, AND PROBABLY ABANDON INIT.SH
all: build test

build:
	clear
	go build -o main cmd/api/main.go

docker-build: test 
	$(DOCKER) build -t gift-registry -f Dockerfile .

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
	go install honnef.co/go/tools/cmd/staticcheck@v0.8.0

local-down:
	clear
	$(DOCKER) compose --env-file=.env_local -f $(DOCKER)-compose.yml down

local-up: test
	$(DOCKER) compose --env-file=.env_local -f $(DOCKER)-compose.yml up -d --no-deps
	$(DOCKER) ps -a

prod-down:
	clear
	$(DOCKER) compose -f $(DOCKER)-compose-prod.yml down

prod-up: docker-build
	$(DOCKER) compose --env-file=.env_prod -f $(DOCKER)-compose-prod.yml up -d --no-deps
	$(DOCKER) ps -a

staticcheck: fmt
	staticcheck ./...

test: staticcheck 
	go test ./...

test-down:
	clear
	$(DOCKER) compose -f $(DOCKER)-compose-test.yml down

test-up: docker-build
	$(DOCKER) compose --env-file=.env_test -f $(DOCKER)-compose-test.yml up -d --no-deps
	$(DOCKER) ps -a
