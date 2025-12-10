package arrays

// UniqueStrings returns a slice of unique strings from the input slice,
// removing duplicates while preserving the order of first occurrence.
func UniqueStrings(input []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(input))
	for _, str := range input {
		if !seen[str] {
			seen[str] = true
			result = append(result, str)
		}
	}
	return result
}
