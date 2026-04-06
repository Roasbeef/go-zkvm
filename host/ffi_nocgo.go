//go:build !cgo

package host

import "errors"

func loadFFILibrary(path string) error {
	return errors.New("host: the FFI-backed host package requires cgo")
}

// ComputeImageID returns an error on non-cgo builds because the host package
// depends on the Rust shared library boundary.
func (c *Client) ComputeImageID(guest []byte) (string, error) {
	return "", errors.New("host: the FFI-backed host package requires cgo")
}

// Execute returns an error on non-cgo builds because the host package depends
// on the Rust shared library boundary.
func (c *Client) Execute(req ExecuteRequest, opts ...RunOption) (*ExecuteResult, error) {
	return nil, errors.New("host: the FFI-backed host package requires cgo")
}

// Prove returns an error on non-cgo builds because the host package depends on
// the Rust shared library boundary.
func (c *Client) Prove(req ProveRequest, opts ...RunOption) (*ProveResult, error) {
	return nil, errors.New("host: the FFI-backed host package requires cgo")
}

// Verify returns an error on non-cgo builds because the host package depends
// on the Rust shared library boundary.
func (c *Client) Verify(req VerifyRequest) (*VerifyResult, error) {
	return nil, errors.New("host: the FFI-backed host package requires cgo")
}
