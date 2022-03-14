GIT_REVISION := $(shell git rev-parse --short HEAD)
GIT_BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
VERSION := "$(shell git describe --tags --abbrev=0)-${GIT_REVISION}"
GIT_OPT := -X main.Branch=$(GIT_BRANCH) -X main.Revision=$(GIT_REVISION) -X main.Version=$(VERSION)
GO_OPT= -ldflags "$(GIT_OPT)"

zombie:
	go build $(GO_OPT) -o ./bin/zombie ./cmd/zombie/main.go

trace-server:
	go build $(GO_OPT) -o ./bin/trace-server ./cmd/trace-server/main.go

all: zombie trace-server

docker: all
	docker build . -t wperron/zombie:latest -f ./cmd/zombie/Dockerfile
	docker build . -t wperron/trace-server:latest -f ./cmd/trace-server/Dockerfile

test:
	go test ./... -v -count=1