GO := $(HOME)/go/bin/go
GOFMT := $(HOME)/go/bin/gofmt
BIN := ./minigit

.PHONY: check vet test fmt examples build clean

# check：静态检查 + 全部测试
check: vet test

vet:
	$(GO) vet ./...

test:
	$(GO) test ./... -count=1

# fmt：检查 gofmt（只检查，不改文件）
fmt:
	@test -z "$$($(GOFMT) -l .)" || { echo "以下文件未 gofmt："; $(GOFMT) -l .; exit 1; }

build:
	$(GO) build -o $(BIN) ./cmd/minigit

# examples：演示 init → hash-object -w → cat-file 全流程，
# 并断言 SHA-1 与真 git 一致、git 能读 minigit 写出的对象。
examples: build
	./scripts/demo.sh "$(CURDIR)/minigit"

clean:
	rm -f $(BIN)
