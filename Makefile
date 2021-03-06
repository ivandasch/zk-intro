.PHONY: help lock disco dependencies
all: help
help: Makefile
	@echo " Choose a command to run :"
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'

## dependencies: install go modules
dependencies:
	go mod download

## watcher: run watcher from lock example
watcher:
	go run -race ./lock watcher

## writer: run writer from lock example. If optional type=naive is set, run without locking. If type=lock, run with locking.
writer:
	go run -race ./lock writer $(type)

## disco: run discovery node
disco:
	go run -race ./disco