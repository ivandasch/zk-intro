.PHONY: help lock disco dependencies
all: help
help: Makefile
	@echo " Choose a command to run :"
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'

## dependencies: install go modules
dependencies:
	go mod download

## watcher-lock: run watcher from lock example
watcher-lock:
	go run -race ./lock watcher

## writer-lock: run writer from lock example. If optional type=fake is set, run without locking.
writer-lock:
	go run -race ./lock writer $(type)

## disco: run discovery node
disco:
	go run -race ./disco