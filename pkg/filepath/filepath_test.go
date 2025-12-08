package filepath_test

import (
	"testing"

	"github.com/kcloutie/scafctl/pkg/filepath"
)

func TestNormalizeFilePath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{
			name: "No colon, no backslash",
			path: "folder/file.txt",
			want: "folder/file.txt",
		},
		{
			name: "Windows backslashes",
			path: "folder\\file.txt",
			want: "folder/file.txt",
		},
		{
			name: "Colon prefix, no backslash",
			path: "prefix:folder/file.txt",
			want: "folder/file.txt",
		},
		{
			name: "Colon prefix, with backslashes",
			path: "prefix:folder\\file.txt",
			want: "folder/file.txt",
		},
		{
			name: "Multiple colons",
			path: "prefix:subprefix:folder\\file.txt",
			want: "subprefix",
		},
		{
			name: "Empty string",
			path: "",
			want: "",
		},
		{
			name: "Colon only",
			path: ":file.txt",
			want: "file.txt",
		},
		{
			name: "Backslash only",
			path: "\\file.txt",
			want: "/file.txt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filepath.NormalizeFilePath(tt.path)
			if got != tt.want {
				t.Errorf("NormalizeFilePath() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJoin(t *testing.T) {
	tests := []struct {
		name  string
		elems []string
		want  string
	}{
		{
			name:  "Simple join",
			elems: []string{"folder", "file.txt"},
			want:  "folder/file.txt",
		},
		{
			name:  "Join with backslashes",
			elems: []string{"folder\\subfolder", "file.txt"},
			want:  "folder/subfolder/file.txt",
		},
		{
			name:  "Join with colon prefix",
			elems: []string{"prefix:folder", "file.txt"},
			want:  "folder/file.txt",
		},
		{
			name:  "Join with multiple elements",
			elems: []string{"a", "b", "c.txt"},
			want:  "a/b/c.txt",
		},
		{
			name:  "Join with empty elements",
			elems: []string{"", "file.txt"},
			want:  "/file.txt",
		},
		{
			name:  "Join with colon only",
			elems: []string{":file.txt"},
			want:  "file.txt",
		},
		{
			name:  "Join with backslash only",
			elems: []string{"\\file.txt"},
			want:  "/file.txt",
		},
		{
			name:  "Join with no elements",
			elems: []string{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filepath.Join(tt.elems...)
			if got != tt.want {
				t.Errorf("Join() = %v, want %v", got, tt.want)
			}
		})
	}
}
