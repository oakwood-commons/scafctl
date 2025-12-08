package filepath

import "strings"

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
