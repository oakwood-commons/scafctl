package filepath_test

import (
	"os"
	"testing"
	"time"

	"github.com/kcloutie/scafctl/pkg/filepath"
	"github.com/kcloutie/scafctl/pkg/fs"
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

type fakeFileInfo struct {
	isDir bool
}

// Implement os.FileInfo interface for fakeFileInfo
func (f fakeFileInfo) Name() string       { return "" }
func (f fakeFileInfo) Size() int64        { return 0 }
func (f fakeFileInfo) Mode() os.FileMode  { return 0 }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.isDir }
func (f fakeFileInfo) Sys() interface{}   { return nil }

func TestIsDirectory(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		statFunc fs.StatFunc
		want     bool
		wantErr  bool
	}{
		{
			name:     "URL path returns false",
			path:     "http://example.com/file.txt",
			statFunc: nil,
			want:     false,
			wantErr:  false,
		},
		{
			name: "Directory path returns true",
			path: "dir",
			statFunc: func(path string) (os.FileInfo, error) {
				return fakeFileInfo{isDir: true}, nil
			},
			want:    true,
			wantErr: false,
		},
		{
			name: "File path returns false",
			path: "file.txt",
			statFunc: func(path string) (os.FileInfo, error) {
				return fakeFileInfo{isDir: false}, nil
			},
			want:    false,
			wantErr: false,
		},
		{
			name: "StatFunc returns error",
			path: "missing.txt",
			statFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			want:    false,
			wantErr: true,
		},
		{
			name:     "Nil statFunc uses os.Stat (should not error for empty string)",
			path:     "",
			statFunc: nil,
			want:     false,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := filepath.IsDirectory(tt.path, tt.statFunc)
			if got != tt.want {
				t.Errorf("IsDirectory() got = %v, want %v", got, tt.want)
			}
			if (err != nil) != tt.wantErr {
				t.Errorf("IsDirectory() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestPathExists(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		statFunc fs.StatFunc
		want     bool
	}{
		{
			name: "Path exists with custom statFunc",
			path: "exists.txt",
			statFunc: func(path string) (os.FileInfo, error) {
				return fakeFileInfo{isDir: false}, nil
			},
			want: true,
		},
		{
			name: "Path does not exist with custom statFunc",
			path: "missing.txt",
			statFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			want: false,
		},
		{
			name: "StatFunc returns other error",
			path: "error.txt",
			statFunc: func(path string) (os.FileInfo, error) {
				return nil, os.ErrPermission
			},
			want: false,
		},
		{
			name:     "Nil statFunc uses os.Stat (should not exist for unlikely file)",
			path:     "unlikely_file_that_should_not_exist_123456789.txt",
			statFunc: nil,
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filepath.PathExists(tt.path, tt.statFunc)
			if got != tt.want {
				t.Errorf("PathExists() = %v, want %v", got, tt.want)
			}
		})
	}
}
