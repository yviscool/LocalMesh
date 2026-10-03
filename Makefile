GO ?= go

.PHONY: fmt test vet check race tidy

fmt:
	@test -z "$$($(GO)fmt -l .)" || (echo "Go files are not formatted"; exit 1)

test:
	$(GO) test ./...

race:
	$(GO) test -race -shuffle=on ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

check: fmt race vet

