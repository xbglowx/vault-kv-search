BUILD_DATE   = $(shell date +%Y%m%d-%H:%M:%S)
BUILD_USER   = $(shell whoami)
GIT_BRANCH   = $(shell git rev-parse --abbrev-ref HEAD)
GIT_REVISION = $(shell git rev-parse HEAD)
LDFLAGS      = -X github.com/xbglowx/vault-kv-search/cmd.BuildDate=$(BUILD_DATE) \
	-X github.com/xbglowx/vault-kv-search/cmd.BuildUser=$(BUILD_USER) \
	-X github.com/xbglowx/vault-kv-search/cmd.Branch=$(GIT_BRANCH) \
	-X github.com/xbglowx/vault-kv-search/cmd.Revision=$(GIT_REVISION) \
	-X github.com/xbglowx/vault-kv-search/cmd.Version=$(VERSION)
OUTPUTOPTION = $(shell test "$(GOOS)" && test "$(GOARCH)" && echo "-o vault-kv-search-$(GOOS)-$(GOARCH)" || echo "")
VERSION      = $(shell git describe --tags $(git rev-list --tags --max-count=1))


.PHONY: all
all: vault-kv-search

vault-kv-search: cmd/*.go
	@go get -v .
	@GOOS=$(GOOS) GOARCH=$(GOARCH) go build -ldflags "$(LDFLAGS)" $(OUTPUTOPTION)

.PHONY: test
test:
	@go test -v ./...

.PHONY: clean
clean:
	@rm -f vault-kv-search*
