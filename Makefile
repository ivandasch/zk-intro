.PHONY: help lock disco dependencies
all: help
help: Makefile
	@echo " Choose a command to run :"
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'

dependencies:
	go mod download

watcher-lock:
	go run -race ./lock watcher

writer-lock:
	go run -race ./lock writer $(type)

disco:
	go run -race ./disco