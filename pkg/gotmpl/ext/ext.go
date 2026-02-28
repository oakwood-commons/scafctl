// Copyright 2025-2026 Oakwood Commons
// SPDX-License-Identifier: Apache-2.0

// Package ext provides the Go template extension function registry.
// It aggregates both third-party (sprig) and custom scafctl-specific
// template functions, making them discoverable via MCP tools and CLI commands.
//
// This follows the same pattern as pkg/celexp/ext for CEL extensions.
package ext

import (
	"sort"
	"text/template"

	sprig "github.com/Masterminds/sprig/v3"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl"
	"github.com/oakwood-commons/scafctl/pkg/gotmpl/ext/hcl"
	extyaml "github.com/oakwood-commons/scafctl/pkg/gotmpl/ext/yaml"
)

// Sprig returns a list of ExtFunction entries for all sprig template functions.
// Each function is wrapped with metadata for discoverability. These are marked
// as Custom=false since they come from the third-party sprig library.
//
// Example usage:
//
//	funcs := ext.Sprig()
//	for _, f := range funcs {
//	    fmt.Printf("Sprig Function: %s\n", f.Name)
//	}
func Sprig() gotmpl.ExtFunctionList {
	funcMap := sprig.FuncMap()

	// Sort keys for deterministic ordering
	keys := make([]string, 0, len(funcMap))
	for k := range funcMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	funcs := make(gotmpl.ExtFunctionList, 0, len(keys))
	for _, name := range keys {
		fn := funcMap[name]
		funcs = append(funcs, gotmpl.ExtFunction{
			Name:        name,
			Description: sprigDescription(name),
			Custom:      false,
			Links:       []string{"https://masterminds.github.io/sprig/"},
			Func: template.FuncMap{
				name: fn,
			},
		})
	}

	return funcs
}

// Custom returns a list of custom scafctl-specific Go template extension functions.
// These are functions with Custom=true that extend Go templates with
// project-specific functionality.
//
// Example usage:
//
//	funcs := ext.Custom()
//	for _, f := range funcs {
//	    fmt.Printf("Custom Function: %s (%s)\n", f.Name, f.Description)
//	}
func Custom() gotmpl.ExtFunctionList {
	return gotmpl.ExtFunctionList{
		// HCL functions
		hcl.ToHclFunc(),

		// YAML functions (removed from Sprig v3.3.0)
		extyaml.ToYamlFunc(),
		extyaml.FromYamlFunc(),
		extyaml.MustToYamlFunc(),
		extyaml.MustFromYamlFunc(),
	}
}

// All returns a combined list of all Go template extension functions, including
// both sprig functions and custom scafctl-specific functions.
// Custom functions are listed after sprig functions so they take precedence
// when merged into a template.FuncMap.
//
// Example usage:
//
//	funcs := ext.All()
//	funcMap := funcs.FuncMap()
func All() gotmpl.ExtFunctionList {
	sprigFuncs := Sprig()
	customFuncs := Custom()

	all := make(gotmpl.ExtFunctionList, 0, len(sprigFuncs)+len(customFuncs))
	all = append(all, sprigFuncs...)
	all = append(all, customFuncs...)

	return all
}

// AllFuncMap is a convenience function that returns a merged template.FuncMap
// containing all extension functions (sprig + custom).
func AllFuncMap() template.FuncMap {
	return All().FuncMap()
}

// sprigDescription returns a short description for well-known sprig functions.
// For functions without a specific description, a generic one is returned.
func sprigDescription(name string) string {
	descriptions := map[string]string{ //nolint:gosec // G101 false positive: not credentials, these are function descriptions
		// String functions
		"trim":                   "Removes leading and trailing whitespace",
		"trimAll":                "Removes all occurrences of a character from a string",
		"trimSuffix":             "Removes a suffix from a string",
		"trimPrefix":             "Removes a prefix from a string",
		"upper":                  "Converts a string to uppercase",
		"lower":                  "Converts a string to lowercase",
		"title":                  "Converts a string to title case",
		"untitle":                "Removes title casing from a string",
		"repeat":                 "Repeats a string N times",
		"substr":                 "Gets a substring",
		"nospace":                "Removes all whitespace from a string",
		"trunc":                  "Truncates a string to a given length",
		"abbrev":                 "Abbreviates a string with ellipses",
		"abbrevboth":             "Abbreviates both sides of a string",
		"initials":               "Gets the initials from a string",
		"randAlphaNum":           "Generates a random alphanumeric string",
		"randAlpha":              "Generates a random alphabetic string",
		"randAscii":              "Generates a random ASCII string",
		"randNumeric":            "Generates a random numeric string",
		"swapcase":               "Swaps the case of a string",
		"shuffle":                "Shuffles the characters in a string",
		"snakecase":              "Converts a string to snake_case",
		"camelcase":              "Converts a string to camelCase",
		"kebabcase":              "Converts a string to kebab-case",
		"wrap":                   "Wraps text at a given column count",
		"wrapWith":               "Wraps text at a given column with a custom delimiter",
		"contains":               "Tests if a string contains a substring",
		"hasPrefix":              "Tests if a string has a given prefix",
		"hasSuffix":              "Tests if a string has a given suffix",
		"quote":                  "Wraps a string in double quotes",
		"squote":                 "Wraps a string in single quotes",
		"cat":                    "Concatenates multiple strings with spaces",
		"indent":                 "Indents every line of a string",
		"nindent":                "Indents every line of a string and prepends a newline",
		"replace":                "Replaces occurrences of a string",
		"plural":                 "Pluralizes a string",
		"sha1sum":                "Generates a SHA-1 hash",
		"sha256sum":              "Generates a SHA-256 hash",
		"adler32sum":             "Generates an Adler-32 checksum",
		"toString":               "Converts a value to a string",
		"toStrings":              "Converts a list of values to a list of strings",
		"join":                   "Joins a list of strings with a separator",
		"splitList":              "Splits a string into a list",
		"split":                  "Splits a string and returns a map",
		"splitn":                 "Splits a string with a limit",
		"regexMatch":             "Tests if a string matches a regular expression",
		"regexFind":              "Finds the first match of a regex",
		"regexFindAll":           "Finds all matches of a regex",
		"regexReplaceAll":        "Replaces all regex matches",
		"regexReplaceAllLiteral": "Replaces all regex matches with a literal string",
		"regexSplit":             "Splits a string by a regex",
		"regexQuoteMeta":         "Quotes regex metacharacters",

		// Math functions
		"add":     "Adds numbers together",
		"add1":    "Adds 1 to a number",
		"sub":     "Subtracts one number from another",
		"div":     "Divides one number by another (integer division)",
		"mod":     "Returns the remainder of integer division",
		"mul":     "Multiplies numbers together",
		"max":     "Returns the largest of a list of integers",
		"min":     "Returns the smallest of a list of integers",
		"floor":   "Returns the greatest integer less than or equal to the value",
		"ceil":    "Returns the least integer greater than or equal to the value",
		"round":   "Rounds a float to a given number of decimal places",
		"len":     "Returns the length of a value",
		"biggest": "Returns the largest of a list of integers (alias for max)",

		// Integer / conversion
		"int":       "Converts a value to int",
		"int64":     "Converts a value to int64",
		"float64":   "Converts a value to float64",
		"toDecimal": "Converts a Unix octal to decimal",
		"atoi":      "Converts a string to an integer",
		"seq":       "Generates a sequence of integers",

		// Date functions
		"now":            "Returns the current time",
		"ago":            "Returns the duration since a time",
		"date":           "Formats a date",
		"dateInZone":     "Formats a date in a given timezone",
		"dateModify":     "Modifies a date by adding a duration",
		"duration":       "Formats a duration",
		"durationRound":  "Rounds a duration to the nearest unit",
		"htmlDate":       "Formats a date for HTML date inputs",
		"htmlDateInZone": "Formats a date for HTML date inputs with timezone",
		"unixEpoch":      "Returns the Unix epoch timestamp",
		"toDate":         "Parses a date string",
		"mustToDate":     "Parses a date string, returning an error on failure",
		"mustDateModify": "Modifies a date, returning an error on failure",

		// Default functions
		"default":          "Returns a default value if the input is empty",
		"empty":            "Returns true if the value is empty",
		"coalesce":         "Returns the first non-empty value",
		"all":              "Returns true if all values are non-empty",
		"any":              "Returns true if any value is non-empty",
		"compact":          "Removes empty values from a list",
		"ternary":          "Returns one of two values based on a boolean",
		"fromJson":         "Decodes a JSON string into an object",
		"toJson":           "Encodes a value as a JSON string",
		"toPrettyJson":     "Encodes a value as a pretty-printed JSON string",
		"toRawJson":        "Encodes a value as a raw JSON string (no HTML escaping)",
		"mustFromJson":     "Decodes JSON, returning an error on failure",
		"mustToJson":       "Encodes to JSON, returning an error on failure",
		"mustToPrettyJson": "Encodes to pretty JSON, returning an error on failure",
		"mustToRawJson":    "Encodes to raw JSON, returning an error on failure",

		// Encoding functions
		"b64enc": "Encodes a string to base64",
		"b64dec": "Decodes a base64 string",
		"b32enc": "Encodes a string to base32",
		"b32dec": "Decodes a base32 string",

		// List functions
		"list":        "Creates a list from arguments",
		"first":       "Returns the first element of a list",
		"rest":        "Returns all but the first element of a list",
		"last":        "Returns the last element of a list",
		"initial":     "Returns all but the last element of a list",
		"append":      "Appends an element to a list",
		"prepend":     "Prepends an element to a list",
		"concat":      "Concatenates lists",
		"reverse":     "Reverses a list",
		"uniq":        "Removes duplicates from a list",
		"without":     "Removes elements from a list",
		"has":         "Tests if a list contains an element",
		"slice":       "Returns a slice of a list",
		"until":       "Generates a list of integers from 0 to N-1",
		"untilStep":   "Generates a list of integers with a step",
		"sortAlpha":   "Sorts a list of strings alphabetically",
		"chunk":       "Splits a list into chunks of a given size",
		"mustAppend":  "Appends to a list, returning an error on failure",
		"mustPrepend": "Prepends to a list, returning an error on failure",
		"mustFirst":   "Returns the first element, returning an error on empty list",
		"mustLast":    "Returns the last element, returning an error on empty list",
		"mustInitial": "Returns all but the last, returning an error on empty list",
		"mustRest":    "Returns all but the first, returning an error on empty list",
		"mustReverse": "Reverses a list, returning an error on failure",
		"mustSlice":   "Returns a slice, returning an error on failure",
		"mustCompact": "Removes empty values, returning an error on failure",
		"mustUniq":    "Removes duplicates, returning an error on failure",
		"mustWithout": "Removes elements, returning an error on failure",
		"mustHas":     "Tests containment, returning an error on failure",
		"mustChunk":   "Splits into chunks, returning an error on failure",

		// Dict functions
		"dict":               "Creates a new dictionary (map)",
		"get":                "Gets a value from a dictionary",
		"set":                "Sets a value in a dictionary",
		"unset":              "Removes a key from a dictionary",
		"hasKey":             "Tests if a dictionary has a key",
		"pluck":              "Extracts values for a key from a list of dictionaries",
		"keys":               "Returns the keys of a dictionary",
		"values":             "Returns the values of a dictionary",
		"pick":               "Creates a new dictionary with only the specified keys",
		"omit":               "Creates a new dictionary without the specified keys",
		"merge":              "Merges two or more dictionaries",
		"mergeOverwrite":     "Merges dictionaries, overwriting existing keys",
		"mustMerge":          "Merges dictionaries, returning an error on failure",
		"mustMergeOverwrite": "Merges with overwrite, returning an error on failure",
		"dig":                "Traverses a nested dictionary by path",
		"deepCopy":           "Creates a deep copy of an object",
		"mustDeepCopy":       "Creates a deep copy, returning an error on failure",

		// Type testing
		"kindOf":     "Returns the kind of a value",
		"kindIs":     "Tests if a value has a specific kind",
		"typeOf":     "Returns the type of a value",
		"typeIs":     "Tests if a value has a specific type",
		"typeIsLike": "Tests if a value has a type similar to a given type",
		"deepEqual":  "Tests deep equality of two values",

		// Crypto functions
		"genPrivateKey":            "Generates a private key (RSA, DSA, ECDSA)",
		"derivePassword":           "Derives a password from a master password",
		"buildCustomCert":          "Builds a custom TLS certificate",
		"genCA":                    "Generates a Certificate Authority",
		"genCAWithKey":             "Generates a CA with a given key",
		"genSelfSignedCert":        "Generates a self-signed certificate",
		"genSelfSignedCertWithKey": "Generates a self-signed certificate with a given key",
		"genSignedCert":            "Generates a signed certificate",
		"genSignedCertWithKey":     "Generates a signed certificate with a given key",
		"encryptAES":               "Encrypts a string with AES-CBC",
		"decryptAES":               "Decrypts an AES-CBC encrypted string",
		"htpasswd":                 "Generates an Apache htpasswd hash",
		"bcrypt":                   "Generates a bcrypt hash",

		// UUID
		"uuidv4": "Generates a UUID v4",

		// OS / environment
		"env":       "Reads an environment variable",
		"expandenv": "Expands environment variables in a string",

		// Path functions
		"base":    "Returns the last element of a path",
		"dir":     "Returns the directory portion of a path",
		"ext":     "Returns the file extension",
		"clean":   "Cleans a path",
		"isAbs":   "Tests if a path is absolute",
		"osBase":  "Returns the last element of a path (OS-specific)",
		"osDir":   "Returns the directory portion of a path (OS-specific)",
		"osExt":   "Returns the file extension (OS-specific)",
		"osClean": "Cleans a path (OS-specific)",
		"osIsAbs": "Tests if a path is absolute (OS-specific)",

		// Semver
		"semver":        "Parses a semantic version string",
		"semverCompare": "Compares two semantic versions",

		// Flow / URL
		"fail":     "Causes the template to fail with an error message",
		"urlParse": "Parses a URL string",
		"urlJoin":  "Joins URL components",

		// Network
		"getHostByName": "Resolves a hostname to an IP address",
	}

	if desc, ok := descriptions[name]; ok {
		return desc
	}

	return "Sprig template function"
}
