HOMEDIR := $(shell pwd)
OUTDIR  := $(HOMEDIR)/output

# init command params
export GO111MODULE=on
ROOT_PATH := $(HOMEDIR)
export ROOT_PATH
export PATH := $(OUTDIR)/bin:$(PATH)

# make clean
cleanall: clean
clean:
	rm -rf $(OUTDIR)