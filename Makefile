GO ?= go

.PHONY: fmt test vet check race tidy protocol-test simulate storage-test transport-test

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

protocol-test:
	$(GO) test -race -shuffle=on ./internal/protocol ./internal/application/idempotency

simulate:
	$(GO) test -race -shuffle=on ./internal/simulation -run TestRunOneHundredAgents -count=1

storage-test:
	$(GO) test -race -shuffle=on ./internal/storage/sqlite -count=1

transport-test:
	$(GO) test -race -shuffle=on ./internal/transport/... -count=1

check: fmt race vet
