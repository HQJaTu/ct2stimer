NAME     := ct2stimer
VERSION  := v0.2.1
REVISION := $(shell git rev-parse --short HEAD)

SRCS      := $(shell find . -type f -name '*.go')
TEMPLATES := $(shell find . -type f -name '*.tmpl')
LDFLAGS   := -ldflags="-s -w -X \"main.Version=$(VERSION)\" -X \"main.Revision=$(REVISION)\" -extldflags \"-static\""

DIST_DIRS := find * -type d -exec

.DEFAULT_GOAL := bin/$(NAME)

# Templates are compiled into the binary via //go:embed, so a changed template
# is just another build input -- no code-generation step is required.
bin/$(NAME): $(SRCS) $(TEMPLATES)
	go build $(LDFLAGS) -o bin/$(NAME)

.PHONY: deps
deps:
	go mod download

.PHONY: test
test:
	go test -cover -race -v ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: fmt
fmt:
	gofmt -w .

.PHONY: clean
clean:
	rm -rf bin/* dist/*

.PHONY: cross-build
cross-build:
	for os in linux; do \
		for arch in amd64 386; do \
			GOOS=$$os GOARCH=$$arch go build -a -tags netgo -installsuffix netgo $(LDFLAGS) -o dist/$$os-$$arch/$(NAME); \
		done; \
	done

.PHONY: dist
dist:
	cd dist && \
	$(DIST_DIRS) cp ../LICENSE {} \; && \
	$(DIST_DIRS) cp ../README.md {} \; && \
	$(DIST_DIRS) tar -zcf $(NAME)-$(VERSION)-{}.tar.gz {} \; && \
	$(DIST_DIRS) zip -r $(NAME)-$(VERSION)-{}.zip {} \; && \
	cd ..

.PHONY: install
install:
	go install $(LDFLAGS)
