GO ?= go
SIBLING_ROOT ?= $(abspath ..)
RISC0_DIR ?= $(SIBLING_ROOT)/risc0
TINYGO_ZKVM_DIR ?= $(SIBLING_ROOT)/tinygo-zkvm
TINYGO_BIN ?= $(TINYGO_ZKVM_DIR)/build/tinygo
GO_GOROOT ?= $(shell $(GO) env GOROOT)
PLATFORM_LIB ?= $(RISC0_DIR)/examples/c-guest/guest/out/platform/riscv32im-risc0-zkvm-elf/release/libzkvm_platform.a
KERNEL ?= $(RISC0_DIR)/risc0/zkos/v1compat/elfs/v1compat.elf
CONVERT := $(GO) run ./convert_to_r0bf.go
GO_GUEST_HOST_DIR ?= $(CURDIR)/go-guest-host

.PHONY: all check-tools clean simple multiply policy-check platform-smoke verify-samples

all: simple multiply policy-check

check-tools:
	@test -x "$(TINYGO_BIN)" || (echo "missing TinyGo binary: $(TINYGO_BIN)" && exit 1)
	@test -f "$(PLATFORM_LIB)" || (echo "missing platform archive: $(PLATFORM_LIB)" && exit 1)
	@test -f "$(KERNEL)" || (echo "missing kernel ELF: $(KERNEL)" && exit 1)

simple: check-tools
	PATH=$(GO_GOROOT)/bin:$$PATH GOROOT=$(GO_GOROOT) $(TINYGO_BIN) build -target=zkvm-platform -scheduler=none -no-debug -ldflags='-extldflags=$(PLATFORM_LIB)' -o simple.elf ./simple
	$(CONVERT) simple.elf $(KERNEL) simple.bin
	@echo "Built simple.bin"

multiply: check-tools
	PATH=$(GO_GOROOT)/bin:$$PATH GOROOT=$(GO_GOROOT) $(TINYGO_BIN) build -target=zkvm-platform -scheduler=none -no-debug -ldflags='-extldflags=$(PLATFORM_LIB)' -o multiply.elf ./multiply
	$(CONVERT) multiply.elf $(KERNEL) multiply.bin
	@echo "Built multiply.bin"

policy-check: check-tools
	PATH=$(GO_GOROOT)/bin:$$PATH GOROOT=$(GO_GOROOT) $(TINYGO_BIN) build -target=zkvm-platform -scheduler=none -no-debug -ldflags='-extldflags=$(PLATFORM_LIB)' -o policy_check.elf ./policy_check
	$(CONVERT) policy_check.elf $(KERNEL) policy_check.bin
	@echo "Built policy_check.bin"

platform-smoke: check-tools
	PATH=$(GO_GOROOT)/bin:$$PATH GOROOT=$(GO_GOROOT) $(TINYGO_BIN) build -target=zkvm-platform -scheduler=none -no-debug -ldflags='-extldflags=$(PLATFORM_LIB)' -o platform_smoke.elf ./platform_smoke
	$(CONVERT) platform_smoke.elf $(KERNEL) platform_smoke.bin
	@echo "Built platform_smoke.bin"

verify-samples: all
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../simple.bin --raw-journal --execute-only
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../simple.bin --raw-journal
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../multiply.bin
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../policy_check.bin --raw-journal --execute-only
	cd $(GO_GUEST_HOST_DIR) && cargo run --release -- ../policy_check.bin --raw-journal

clean:
	rm -f *.elf *.bin
