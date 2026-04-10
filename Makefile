GO ?= go
GOCC ?= $(GO)
TOOLS_DIR := tools
LOCAL_CUSTOM_GCL := $(CURDIR)/$(TOOLS_DIR)/custom-gcl
GOLANGCI_LINT_VERSION := v1.64.8
SIBLING_ROOT ?= $(abspath ..)
RISC0_DIR ?= $(SIBLING_ROOT)/risc0
TINYGO_ZKVM_DIR ?= $(SIBLING_ROOT)/tinygo-zkvm
TINYGO_BIN ?= $(TINYGO_ZKVM_DIR)/build/tinygo
GO_GOROOT ?= $(shell $(GO) env GOROOT)
PLATFORM_LIB ?= $(RISC0_DIR)/examples/c-guest/guest/out/platform/riscv32im-risc0-zkvm-elf/release/libzkvm_platform.a
KERNEL ?= $(RISC0_DIR)/risc0/zkos/v1compat/elfs/v1compat.elf
CONVERT := $(GO) run ./convert_to_r0bf.go
GO_GUEST_HOST_DIR ?= $(CURDIR)/go-guest-host
HOST_FFI_DIR ?= $(CURDIR)/host-ffi
EXAMPLES_DIR ?= $(CURDIR)/examples
GOFMT_FILES := $(shell find . -type f -name '*.go' -not -path './go-guest-host/target/*' -not -path './host-ffi/target/*' -not -path './vendor/*')
NATIVE_GO_PKGS := ./host

GREEN := "\\033[0;32m"
NC := "\\033[0m"
define print
	@echo $(GREEN)$1$(NC)
endef

.PHONY: all check-tools clean platform-standalone simple multiply policy-check verify-samples host-ffi test-host-ffi fmt fmt-check tidy tidy-check local-custom-gcl install-custom-gcl build-native-linter lint-native lint native-check

all: simple multiply policy-check

platform-standalone:
	$(MAKE) -C $(RISC0_DIR)/examples/c-guest platform-standalone

check-tools:
	@test -x "$(TINYGO_BIN)" || (echo "missing TinyGo binary: $(TINYGO_BIN)" && exit 1)
	@test -f "$(PLATFORM_LIB)" || (echo "missing platform archive: $(PLATFORM_LIB)" && exit 1)
	@test -f "$(KERNEL)" || (echo "missing kernel ELF: $(KERNEL)" && exit 1)

simple: check-tools
	PATH=$(GO_GOROOT)/bin:$$PATH GOROOT=$(GO_GOROOT) $(TINYGO_BIN) build -target=zkvm-platform -scheduler=none -no-debug -ldflags='-extldflags=$(PLATFORM_LIB)' -o simple.elf $(EXAMPLES_DIR)/simple
	$(CONVERT) simple.elf $(KERNEL) simple.bin
	@echo "Built simple.bin"

multiply: check-tools
	PATH=$(GO_GOROOT)/bin:$$PATH GOROOT=$(GO_GOROOT) $(TINYGO_BIN) build -target=zkvm-platform -scheduler=none -no-debug -ldflags='-extldflags=$(PLATFORM_LIB)' -o multiply.elf $(EXAMPLES_DIR)/multiply
	$(CONVERT) multiply.elf $(KERNEL) multiply.bin
	@echo "Built multiply.bin"

policy-check: check-tools
	PATH=$(GO_GOROOT)/bin:$$PATH GOROOT=$(GO_GOROOT) $(TINYGO_BIN) build -target=zkvm-platform -scheduler=none -no-debug -ldflags='-extldflags=$(PLATFORM_LIB)' -o policy_check.elf $(EXAMPLES_DIR)/policy_check
	$(CONVERT) policy_check.elf $(KERNEL) policy_check.bin
	@echo "Built policy_check.bin"

verify-samples: all
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../simple.bin --raw-journal --execute-only
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../simple.bin --raw-journal
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../multiply.bin
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../policy_check.bin --raw-journal --execute-only
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../policy_check.bin --raw-journal

host-ffi:
	cargo build --manifest-path $(HOST_FFI_DIR)/Cargo.toml --release

test-host-ffi: host-ffi simple
	CGO_ENABLED=1 $(GO) test ./host -v

fmt:
	gofmt -w $(GOFMT_FILES)

fmt-check:
	@test -z "$$(gofmt -l $(GOFMT_FILES))" || (gofmt -l $(GOFMT_FILES) && exit 1)

tidy:
	$(GO) mod tidy

tidy-check: tidy
	@test -z "$$(git status --porcelain -- go.mod go.sum)" || (git status --short -- go.mod go.sum && exit 1)

local-custom-gcl:
	@./scripts/local-custom-gcl.sh "$(LOCAL_CUSTOM_GCL)" "$(GOLANGCI_LINT_VERSION)"

install-custom-gcl:
	@./scripts/install-custom-gcl.sh "$(if $(dest),$(dest),$(LOCAL_CUSTOM_GCL))" "$(GOLANGCI_LINT_VERSION)"

build-native-linter:
	@$(call print, "Building native linter binary.")
	@./scripts/install-custom-gcl.sh "$(LOCAL_CUSTOM_GCL)" "$(GOLANGCI_LINT_VERSION)"

lint-native: build-native-linter
	@$(call print, "Linting source (native).")
	GOWORK=off $(LOCAL_CUSTOM_GCL) run -v --timeout=10m $(NATIVE_GO_PKGS)

lint: lint-native

native-check: host-ffi simple
	$(GO) build ./convert_to_r0bf.go
	$(GO) build ./extract_r0bf.go
	$(GO) build ./host
	CGO_ENABLED=1 $(GO) test ./host -run TestHostFFISimpleGuest -v

clean:
	rm -f *.elf *.bin
	rm -f convert_to_r0bf extract_r0bf
