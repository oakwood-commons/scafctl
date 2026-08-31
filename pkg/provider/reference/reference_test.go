package reference

import (
	"errors"
	"strings"
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		name     string
		in       Reference
		wantPath string
		wantName string
		wantErr  error
	}{
		// Fully qualified
		{"canonical", "ghcr.io/oakwood-commons/github", "ghcr.io/oakwood-commons", "github", nil},
		{"nested namespace", "ghcr.io/a/b/c/github", "ghcr.io/a/b/c", "github", nil},
		{"two segments", "ghcr.io/github", "ghcr.io", "github", nil},
		{"registry with port", "localhost:5000/team/tool", "localhost:5000/team", "tool", nil},
		{"dots and dashes in name", "ghcr.io/oakwood-commons/my.tool-v2", "ghcr.io/oakwood-commons", "my.tool-v2", nil},

		// Short names
		{"short name", "github", "", "github", nil},
		{"short name with dash", "my-tool", "", "my-tool", nil},

		// Whitespace is trimmed
		{"padded qualified", "  ghcr.io/oakwood-commons/github  ", "ghcr.io/oakwood-commons", "github", nil},
		{"padded short name", "\tgithub\n", "", "github", nil},

		// Invalid
		{"empty", "", "", "", ErrInvalidReference},
		{"whitespace only", "   ", "", "", ErrInvalidReference},
		{"leading slash", "/github", "", "", ErrInvalidReference},
		{"trailing slash", "ghcr.io/", "", "", ErrInvalidReference},
		{"trailing slash on qualified", "ghcr.io/oakwood-commons/", "", "", ErrInvalidReference},
		{"slash only", "/", "", "", ErrInvalidReference},
		{"double slash only", "//", "", "", ErrInvalidReference},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, name, err := tt.in.Split()

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Split(%q) err = %v, want %v", tt.in, err, tt.wantErr)
			}
			if path != tt.wantPath {
				t.Errorf("Split(%q) registryPath = %q, want %q", tt.in, path, tt.wantPath)
			}
			if name != tt.wantName {
				t.Errorf("Split(%q) artifactName = %q, want %q", tt.in, name, tt.wantName)
			}
		})
	}
}

// Documents current (lax) behaviour for empty interior segments, so a future
// tightening of the rules shows up as a deliberate test change rather than a
// silent behaviour shift.
func TestSplit_EmptyInteriorSegment(t *testing.T) {
	path, name, err := Reference("ghcr.io//github").Split()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "ghcr.io/" || name != "github" {
		t.Errorf(`got (%q, %q), want ("ghcr.io/", "github")`, path, name)
	}
}

func TestAccessors(t *testing.T) {
	tests := []struct {
		in            Reference
		wantPath      string
		wantName      string
		wantShortName bool
		wantValid     bool
	}{
		{"ghcr.io/oakwood-commons/github", "ghcr.io/oakwood-commons", "github", false, true},
		{"ghcr.io/a/b/c/github", "ghcr.io/a/b/c", "github", false, true},
		{"github", "", "github", true, true},
		{"", "", "", false, false},
		{"/github", "", "", false, false},
		{"ghcr.io/", "", "", false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.in), func(t *testing.T) {
			if got := tt.in.RegistryPath(); got != tt.wantPath {
				t.Errorf("RegistryPath() = %q, want %q", got, tt.wantPath)
			}
			if got := tt.in.ArtifactName(); got != tt.wantName {
				t.Errorf("ArtifactName() = %q, want %q", got, tt.wantName)
			}
			if got := tt.in.IsShortName(); got != tt.wantShortName {
				t.Errorf("IsShortName() = %v, want %v", got, tt.wantShortName)
			}
			if got := tt.in.Valid(); got != tt.wantValid {
				t.Errorf("Valid() = %v, want %v", got, tt.wantValid)
			}
		})
	}
}

// IsShortName must be false for invalid references, not just for qualified ones.
func TestIsShortName_InvalidIsNotShort(t *testing.T) {
	for _, in := range []Reference{"", "   ", "/", "/github", "ghcr.io/"} {
		if in.IsShortName() {
			t.Errorf("IsShortName(%q) = true, want false", in)
		}
	}
}

func TestString(t *testing.T) {
	// String is the raw value, untrimmed and unmodified.
	for _, in := range []Reference{"ghcr.io/oakwood-commons/github", "github", "  padded  ", ""} {
		if got := in.String(); got != string(in) {
			t.Errorf("String() = %q, want %q", got, string(in))
		}
	}
}

func TestValidate(t *testing.T) {
	valid := []Reference{
		"github",
		"my-tool",
		"ghcr.io/github",
		"ghcr.io/oakwood-commons/github",
		"ghcr.io/a/b/c/github",
		"localhost:5000/team/tool",
	}
	for _, in := range valid {
		if err := in.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", in, err)
		}
	}

	invalid := []Reference{
		"",                               // empty
		"   ",                            // whitespace only
		"/github",                        // leading slash
		"ghcr.io/",                       // trailing slash
		"ghcr.io//github",                // empty interior segment
		"ghcr.io/org/parameter@^1.0.0",   // inline version constraint
		"github@1.0.0",                   // inline version on short name
		"ghcr.io/org/tool@sha256:abcdef", // inline digest
		"ghcr.io/org/UPPER",              // uppercase leaf
	}
	for _, in := range invalid {
		if err := in.Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want error", in)
		} else if !errors.Is(err, ErrInvalidReference) {
			t.Errorf("Validate(%q) error = %v, want ErrInvalidReference", in, err)
		}
	}
}

func FuzzSplit(f *testing.F) {
	seeds := []string{
		"ghcr.io/oakwood-commons/github",
		"github",
		"",
		"/",
		"//",
		"ghcr.io/",
		"localhost:5000/team/tool",
		"  padded/name  ",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, s string) {
		path, name, err := Reference(s).Split()
		if err != nil {
			// On error both outputs must be zeroed.
			if path != "" || name != "" {
				t.Fatalf("Split(%q) returned (%q, %q) alongside error", s, path, name)
			}
			return
		}

		if name == "" {
			t.Fatalf("Split(%q) succeeded with empty artifactName", s)
		}
		if strings.Contains(name, "/") {
			t.Fatalf("Split(%q) artifactName %q contains a slash", s, name)
		}

		// The parts must reassemble into the trimmed input.
		trimmed := strings.TrimSpace(s)
		rejoined := name
		if path != "" {
			rejoined = path + "/" + name
		}
		if rejoined != trimmed {
			t.Fatalf("Split(%q) parts rejoin to %q, want %q", s, rejoined, trimmed)
		}
	})
}

func BenchmarkSplit(b *testing.B) {
	const ref Reference = "ghcr.io/oakwood-commons/github"

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		sinkPath, sinkName, _ = ref.Split()
	}
}

// Package-level sinks keep the benchmark's results from being optimised away.
var sinkPath, sinkName string
