GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
BINARY_NAME=bin/valar

all: build

build:
	mkdir -p bin/
	$(GOBUILD) -ldflags "-X valar/cli/cmd.version=indev" -o $(BINARY_NAME) -v

test: 
	$(GOTEST) -v ./...

clean: 
	$(GOCLEAN)
	rm -f $(BINARY_NAME)

install:
	cp $(BINARY_NAME) /usr/local/bin/
