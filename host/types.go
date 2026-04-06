package host

import (
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	abiVersion = 1
	// LibraryPathEnvVar is the optional environment variable used to
	// override the default `host-ffi` shared-library lookup path.
	LibraryPathEnvVar = "GO_ZKVM_HOST_LIBRARY_PATH"
)

// Client is the Go-facing wrapper around the Rust host FFI boundary.
type Client struct {
	libraryPath string
}

type clientConfig struct {
	libraryPath string
}

// ClientOption mutates the construction-time configuration for a host Client.
type ClientOption func(*clientConfig)

type runConfig struct {
	logger            *slog.Logger
	receiptSelfVerify bool
}

// RunOption mutates per-call execution or proving behavior.
type RunOption func(*runConfig)

// ExecuteRequest describes one execute-only run of a packaged guest binary.
type ExecuteRequest struct {
	// GuestBinary is the packaged guest `.bin` artifact to execute.
	GuestBinary []byte
	// Stdin is the raw private witness stream fed into guest stdin.
	Stdin []byte
}

// ExecuteResult summarizes the public output of an execute-only run.
type ExecuteResult struct {
	// ImageID is the computed image ID for the loaded guest.
	ImageID string
	// Journal is the raw committed public journal.
	Journal []byte
	// ExitCode is the guest exit summary reported by the executor.
	ExitCode string
	// SegmentCount is the number of zkVM segments executed.
	SegmentCount uint32
	// SessionRows is the total row count reported by the session.
	SessionRows uint64
}

// ProveRequest describes one prove run of a packaged guest binary.
type ProveRequest struct {
	// GuestBinary is the packaged guest `.bin` artifact to prove.
	GuestBinary []byte
	// Stdin is the raw private witness stream fed into guest stdin.
	Stdin []byte
}

// ProveResult contains the public claim plus the serialized receipt.
type ProveResult struct {
	// ImageID is the computed image ID for the loaded guest.
	ImageID string
	// Journal is the raw committed public journal.
	Journal []byte
	// Receipt is the serialized risc0 receipt bytes.
	Receipt []byte
	// ReceiptEncoding names the serialized receipt encoding.
	ReceiptEncoding string
	// ProverName identifies the selected proving backend.
	ProverName string
	// SealBytes is the proof seal size in bytes.
	SealBytes uint64
}

// VerifyRequest describes one receipt verification operation.
type VerifyRequest struct {
	// Receipt is the serialized receipt bytes to verify.
	Receipt []byte
	// ImageID is the expected guest image ID.
	ImageID string
	// ExpectedJournal optionally checks the committed journal bytes too.
	ExpectedJournal []byte
}

// VerifyResult contains the verified journal plus receipt metadata.
type VerifyResult struct {
	// Verified reports whether verification succeeded.
	Verified bool
	// Journal is the verified raw committed public journal.
	Journal []byte
	// ReceiptEncoding names the serialized receipt encoding.
	ReceiptEncoding string
	// SealBytes is the proof seal size in bytes.
	SealBytes uint64
}

// HostError is the structured error returned by the host wrapper.
type HostError struct {
	// Op identifies the host operation that failed.
	Op string
	// Code is the stable machine-readable error code when available.
	Code string
	// Message is the human-readable failure detail.
	Message string
}

// Error returns the formatted host error string.
func (e *HostError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return e.Op + ": " + e.Message
	}
	return e.Op + " (" + e.Code + "): " + e.Message
}

// WithLibraryPath overrides the shared-library lookup path used by New. This
// takes precedence over LibraryPathEnvVar and the sibling-layout fallback.
func WithLibraryPath(path string) ClientOption {
	return func(cfg *clientConfig) {
		cfg.libraryPath = path
	}
}

// WithLogger adds a logger that receives high-level call metadata.
func WithLogger(logger *slog.Logger) RunOption {
	return func(cfg *runConfig) {
		cfg.logger = logger
	}
}

// WithReceiptSelfVerify toggles the Rust-side receipt self-check during Prove.
func WithReceiptSelfVerify(enabled bool) RunOption {
	return func(cfg *runConfig) {
		cfg.receiptSelfVerify = enabled
	}
}

// New constructs a host Client and loads the shared library immediately.
func New(opts ...ClientOption) (*Client, error) {
	cfg := clientConfig{
		libraryPath: resolvedLibraryPath(""),
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.libraryPath == "" {
		return nil, errors.New("host: library path must not be empty")
	}

	if err := loadFFILibrary(cfg.libraryPath); err != nil {
		return nil, err
	}

	return &Client{
		libraryPath: cfg.libraryPath,
	}, nil
}

// Close releases the Client. The current implementation has no extra teardown
// work beyond matching the future-friendly API shape.
func (c *Client) Close() error {
	return nil
}

// ReadGuestFile reads a packaged guest `.bin` artifact from disk.
func ReadGuestFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// ComputeImageIDFile reads a guest artifact from disk and computes its image
// ID through the Rust host stack.
func ComputeImageIDFile(path string) (string, error) {
	guest, err := ReadGuestFile(path)
	if err != nil {
		return "", err
	}

	client, err := New()
	if err != nil {
		return "", err
	}
	defer client.Close()

	return client.ComputeImageID(guest)
}

// ExecuteFile is a convenience wrapper that reads a guest artifact and then
// executes it with the provided private stdin bytes.
func ExecuteFile(
	path string, stdin []byte, opts ...RunOption,
) (*ExecuteResult, error) {

	guest, err := ReadGuestFile(path)
	if err != nil {
		return nil, err
	}

	client, err := New()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.Execute(ExecuteRequest{
		GuestBinary: guest,
		Stdin:       stdin,
	}, opts...)
}

// ProveFile is a convenience wrapper that reads a guest artifact and then
// proves it with the provided private stdin bytes.
func ProveFile(
	path string, stdin []byte, opts ...RunOption,
) (*ProveResult, error) {

	guest, err := ReadGuestFile(path)
	if err != nil {
		return nil, err
	}

	client, err := New()
	if err != nil {
		return nil, err
	}
	defer client.Close()

	return client.Prove(ProveRequest{
		GuestBinary: guest,
		Stdin:       stdin,
	}, opts...)
}

func defaultLibraryPath() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}

	dir := filepath.Dir(file)
	libName := "libgo_zkvm_host.so"
	if runtime.GOOS == "darwin" {
		libName = "libgo_zkvm_host.dylib"
	}

	return filepath.Clean(
		filepath.Join(
			dir, "..", "host-ffi", "target", "release", libName,
		),
	)
}

func resolvedLibraryPath(explicitPath string) string {
	if strings.TrimSpace(explicitPath) != "" {
		return explicitPath
	}

	if envPath, ok := os.LookupEnv(LibraryPathEnvVar); ok &&
		strings.TrimSpace(envPath) != "" {

		return envPath
	}

	return defaultLibraryPath()
}

func defaultRunConfig() runConfig {
	return runConfig{
		receiptSelfVerify: true,
	}
}
