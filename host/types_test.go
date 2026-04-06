package host

import "testing"

func TestResolvedLibraryPathPrefersExplicitOverride(t *testing.T) {
	t.Setenv(LibraryPathEnvVar, "/tmp/from-env/libgo_zkvm_host.dylib")

	got := resolvedLibraryPath("/tmp/from-opt/libgo_zkvm_host.dylib")
	want := "/tmp/from-opt/libgo_zkvm_host.dylib"
	if got != want {
		t.Fatalf(
			"resolved library path mismatch: got %q want %q",
			got, want,
		)
	}
}

func TestResolvedLibraryPathUsesEnvOverride(t *testing.T) {
	t.Setenv(LibraryPathEnvVar, "/tmp/from-env/libgo_zkvm_host.dylib")

	got := resolvedLibraryPath("")
	want := "/tmp/from-env/libgo_zkvm_host.dylib"
	if got != want {
		t.Fatalf(
			"resolved library path mismatch: got %q want %q",
			got, want,
		)
	}
}

func TestResolvedLibraryPathFallsBackToSiblingLayout(t *testing.T) {
	t.Setenv(LibraryPathEnvVar, "")

	got := resolvedLibraryPath("")
	want := defaultLibraryPath()
	if got != want {
		t.Fatalf(
			"resolved library path mismatch: got %q want %q",
			got, want,
		)
	}
}
