GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean

VERSION ?= $(shell git describe --tags --always --dirty 2> /dev/null )
LDFLAGS=-ldflags "-X=main.version=$(VERSION)"

cmds=$(patsubst %.go, %, $(wildcard cmd/*/*.go))

all: $(cmds)

$(cmds) : % : %.go
	cd $(@D); \
	$(GOBUILD) $(LDFLAGS)

clean: 
	$(GOCLEAN) ./...

