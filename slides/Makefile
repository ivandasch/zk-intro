.PHONY: help
all: help
help: Makefile
	@echo " Choose a command to run :"
	@sed -n 's/^##//p' $< | column -t -s ':' |  sed -e 's/^/ /'

## docker-image: build local docker image of asciidoctor
docker-image:
	docker build -t local-docker-asciidoctor --build-arg MYUSER=$(USER) .

PWD = $(shell pwd)
REVEALJS_CDN= "https://cdnjs.cloudflare.com/ajax/libs/reveal.js/3.9.2"
## create-slides: build slides
create-slides:
	docker run -t -v $(PWD):/home/$(USER) local-docker-asciidoctor /bin/bash -c  \
	"asciidoctor-revealjs -a revealjsdir=$(REVEALJS_CDN) -r asciidoctor-diagram zk-slides.adoc -o index.html"

## clean: clean untracked files
clean:
	git clean -f -d -x $(PWD)

## slides: build docker image and slides
slides: docker-image create-slides


