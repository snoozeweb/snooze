#!make

DOCKER=docker
IMAGE_NAME=snoozeweb/snooze-server

.DEFAULT_GOAL:=help

-include .env .env.local .env.*.local

VCS_REF=$(git rev-parse --short HEAD)
BUILD_DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
VERSION:=$(cat VERSION)

ifndef IMAGE_NAME
$(error IMAGE_NAME is not set)
endif

.PHONY: version

all:	help

## lint			- Lint Dockerfile.
lint:
	docker run --rm -i hadolint/hadolint < Dockerfile

## build			- Build docker image.
##	-t $(IMAGE_NAME):$(VERSION) \
##	-t $(IMAGE_NAME):$(VCS_REF) \
build:
	$(DOCKER) build \
	--build-arg VCS_REF=$(VCS_REF) \
	--build-arg BUILD_DATE=$(BUILD_DATE) \
	--build-arg VERSION=$(VERSION) \
	-t $(IMAGE_NAME) \
	-t $(IMAGE_NAME):latest .

## push			- Push docker image to repository.
push:
	$(DOCKER) push $(IMAGE_NAME)

## env			- Print environment variables.
env:
	env

## help			- Show this help.
help: Makefile
	@echo ''
	@echo 'Usage:'
	@echo '  make [TARGET]'
	@echo ''
	@echo 'Targets:'
	@sed -n 's/^##//p' $<
	@echo ''

	@echo 'Add project-specific env variables to .env file:'
	@echo 'PROJECT=$(PROJECT)'

.PHONY: help lint test build sdist wheel clean all
