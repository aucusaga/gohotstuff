# init command params
HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output
CONFDIR := $(HOMEDIR)/conf

GOROOT  := $(shell go env GOROOT)
GO      := $(GOROOT)/bin/go
GOPATH  := $(shell $(GO) env GOPATH)
GOMOD   := $(GO) mod
GOBUILD := $(GO) build
GOTEST  := $(GO) test -gcflags="-N -l"
GOPKGS  := $$($(GO) list ./...| grep -vE "vendor")
VERSION := 1.0.0

export GO111MODULE=on
ROOT_PATH := $(HOMEDIR)
export ROOT_PATH
export PATH := $(OUTDIR)/bin:$(PATH)

all: clean gomod build

clean:
	rm -rf $(OUTDIR)

gomod:
	$(GOMOD) tidy

build:
	mkdir $(OUTDIR)
	cp -r $(CONFDIR) $(OUTDIR)/
	$(GOBUILD) -o $(OUTDIR)/gohotstuff $(HOMEDIR)/gohotstuff/main.go
