.PHONY: build run start stop

# Fetching private IP based on OS
UNAME_S := $(shell uname -s)
ifeq ($(UNAME_S),Linux)
    IP=$(shell hostname -I | awk '{print $$1}')
endif
ifeq ($(UNAME_S),Darwin)    # Mac OS
    IP=0.0.0.0
endif

NETWORK_NAME := stress-network

build:
	@echo "Building Docker image..."
	@docker build -t carbonifer-stress .

stop:
	@echo "Stopping and removing Docker containers..."
	@docker stop consul-server consul-agent carbonifer-stress || true
	@docker rm consul-server consul-agent carbonifer-stress || true
	@echo "Removing Docker network..."
	@docker network rm $(NETWORK_NAME) || true

start: stop build
	@echo "Creating Docker network..."
	@docker network create $(NETWORK_NAME) || true
	@echo "Starting Consul Server..."
	@docker run -d --network=$(NETWORK_NAME) --name=consul-server -p 8300:8300 -p 8301:8301 -p 8302:8302 -p 8400:8400 -p 8500:8500 consul:1.10.1 agent -server -bootstrap-expect=1 -ui -bind=0.0.0.0 -client=0.0.0.0
	@echo "Starting Consul Agent..."
	@docker run -d --network=$(NETWORK_NAME) --name=consul-agent -p 9300:8300 -p 9301:8301 -p 9302:8302 -p 9400:8400 -p 9500:8500 consul:1.10.1 agent -bind=0.0.0.0 -retry-join=consul-server -client=0.0.0.0
	@echo "Starting Carbonifer Stress..."
	@docker run -d --network=$(NETWORK_NAME) --name=carbonifer-stress -p 8080:8080 carbonifer-stress

all: build start
