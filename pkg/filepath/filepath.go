// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

package filepath

import (
	"errors"
	"net/url"
	"os"
	"strings"

	"github.com/oakwood-commons/scafctl/pkg/fs"
)

// NormalizeFilePath normalizes a file path by removing any prefix before a colon (':'),
// and replacing all backslashes ('\') with forward slashes ('/'). This is useful for
// ensuring consistent file path formatting across different operating systems.
func NormalizeFilePath(path string) string {
	if strings.Contains(path, ":") {
		path = strings.Split(path, ":")[1]
	}
	return strings.ReplaceAll(path, "\\", "/")
}

// Join concatenates the provided path elements into a single file path,
// separating them with a forward slash ('/'), and normalizes the resulting path.
// It returns the normalized file path as a string.
func Join(elem ...string) string {
	return NormalizeFilePath(strings.Join(elem, "/"))
}

// IsDirectory checks whether the given path refers to a directory.
// If the path is a URL, it returns false with no error.
// The statFunc parameter allows for custom file stat implementations; if nil, os.Stat is used.
// Returns true if the path is a directory, false otherwise, along with any error encountered.
func IsDirectory(path string, statFunc fs.StatFunc) (bool, error) {
	if IsURL(path) {
		return false, nil
	}
	if statFunc == nil {
		statFunc = os.Stat
	}
	fileInfo, err := statFunc(path)
	if err != nil {
		return false, err
	}

	return fileInfo.IsDir(), err
}

// PathExists checks whether the specified file or directory exists at the given path.
// It uses the provided statFunc to retrieve file information. If statFunc is nil,
// os.Stat is used by default. Returns true if the path exists, false otherwise.
func PathExists(path string, statFunc fs.StatFunc) bool {
	if statFunc == nil {
		statFunc = os.Stat
	}
	if _, err := statFunc(path); err == nil {
		return true
	} else if errors.Is(err, os.ErrNotExist) {
		return false
	}
	return false
}

// IsURL checks whether the given path string is a valid HTTP or HTTPS URL.
// It returns true if the path starts with "http" (case-insensitive) and can be parsed as a valid URL,
// otherwise it returns false.
func IsURL(path string) bool {
	if !strings.HasPrefix(strings.ToLower(path), "http") {
		return false
	}
	_, err := url.ParseRequestURI(path)
	return err == nil
}
