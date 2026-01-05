package ext

import (
	"github.com/google/cel-go/cel"
	celext "github.com/google/cel-go/ext"
	"github.com/oakwood-commons/scafctl/pkg/celexp"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext/arrays"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext/debug"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext/filepath"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext/guid"
	celmap "github.com/oakwood-commons/scafctl/pkg/celexp/ext/map"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext/marshalling"
	"github.com/oakwood-commons/scafctl/pkg/celexp/ext/out"
	celsort "github.com/oakwood-commons/scafctl/pkg/celexp/ext/sort"
	celstrings "github.com/oakwood-commons/scafctl/pkg/celexp/ext/strings"
	celtime "github.com/oakwood-commons/scafctl/pkg/celexp/ext/time"
)

// BuiltIn returns a list of built-in CEL extension functions provided by the
// google/cel-go library. These include extensions for strings, lists, bindings,
// encoders, math, protos, sets, and various CEL options.
//
// Example usage:
//
//	funcs := ext.BuiltIn()
//	for _, f := range funcs {
//	    fmt.Printf("Extension: %s (%s)\n", f.Name, f.Description)
//	}
func BuiltIn() celexp.ExtFunctionList {
	funcs := celexp.ExtFunctionList{
		{
			Name:        "strings",
			Links:       []string{"https://github.com/google/cel-go/blob/master/ext/strings.go"},
			Description: "String manipulation functions",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "charAt - Get character at position",
					Expression:  `"hello".charAt(1)`,
				},
				{
					Description: "format - String substitution (printf-style)",
					Expression:  `"Hello %s, you are %d years old".format(["Alice", 30])`,
				},
				{
					Description: "indexOf - Find first occurrence of substring",
					Expression:  `"hello mellow".indexOf("ello")`,
				},
				{
					Description: "indexOf - Find occurrence with start offset",
					Expression:  `"hello mellow".indexOf("ello", 2)`,
				},
				{
					Description: "join - Concatenate list of strings",
					Expression:  `["a", "b", "c"].join(",")`,
				},
				{
					Description: "lastIndexOf - Find last occurrence",
					Expression:  `"hello mellow".lastIndexOf("ello")`,
				},
				{
					Description: "lowerAscii - Convert ASCII to lowercase",
					Expression:  `"TacoCat".lowerAscii()`,
				},
				{
					Description: "strings.quote - Make string safe to print",
					Expression:  `strings.quote("line1\nline2")`,
				},
				{
					Description: "replace - Replace all occurrences",
					Expression:  `"hello hello".replace("he", "we")`,
				},
				{
					Description: "replace - Replace with limit",
					Expression:  `"hello hello".replace("he", "we", 1)`,
				},
				{
					Description: "reverse - Reverse string characters",
					Expression:  `"hello".reverse()`,
				},
				{
					Description: "split - Split string by delimiter",
					Expression:  `"a,b,c".split(",")`,
				},
				{
					Description: "split - Split with limit",
					Expression:  `"a,b,c,d".split(",", 2)`,
				},
				{
					Description: "substring - Extract from position to end",
					Expression:  `"tacocat".substring(4)`,
				},
				{
					Description: "substring - Extract range",
					Expression:  `"tacocat".substring(0, 4)`,
				},
				{
					Description: "trim - Remove leading/trailing whitespace",
					Expression:  `"  hello  ".trim()`,
				},
				{
					Description: "upperAscii - Convert ASCII to uppercase",
					Expression:  `"TacoCat".upperAscii()`,
				},
			},
			EnvOptions: []cel.EnvOption{celext.Strings(celext.StringsVersion(4))},
		},
		{
			Name:        "lists",
			Links:       []string{"https://github.com/google/cel-go/blob/master/ext/lists.go"},
			Description: "List manipulation functions",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "distinct - Get unique elements (v2)",
					Expression:  `[1, 2, 2, 3, 3, 3].distinct()`,
				},
				{
					Description: "distinct - Preserve order of first occurrence",
					Expression:  `["b", "b", "c", "a", "c"].distinct()`,
				},
				{
					Description: "flatten - Flatten nested lists (one level)",
					Expression:  `[1, [2, 3], [4]].flatten()`,
				},
				{
					Description: "flatten - Flatten with depth limit",
					Expression:  `[1, [2, [3, [4]]]].flatten(2)`,
				},
				{
					Description: "lists.range - Generate sequence from 0 to n-1 (v2)",
					Expression:  `lists.range(5)`,
				},
				{
					Description: "reverse - Reverse list order (v1)",
					Expression:  `[1, 2, 3, 4].reverse()`,
				},
				{
					Description: "slice - Extract sublist with start and end indices",
					Expression:  `[1, 2, 3, 4, 5].slice(1, 3)`,
				},
				{
					Description: "sort - Sort list with comparable elements (v2)",
					Expression:  `[3, 1, 4, 1, 5].sort()`,
				},
				{
					Description: "sort - Sort strings alphabetically",
					Expression:  `["foo", "bar", "baz"].sort()`,
				},
				{
					Description: "sortBy - Sort objects by property (v2)",
					Expression:  `[{"name": "foo", "score": 0}, {"name": "bar", "score": -10}, {"name": "baz", "score": 1000}].sortBy(e, e.score).map(e, e.name)`,
				},
			},
			EnvOptions: []cel.EnvOption{celext.Lists(celext.ListsVersion(3))},
		},
		{
			Name:        "bindings",
			Links:       []string{"https://github.com/google/cel-go/blob/master/ext/bindings.go"},
			Description: "Dynamic bindings for local variables in expressions",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "cel.bind - Simple variable binding",
					Expression:  `cel.bind(x, "hello", x + " world")`,
				},
				{
					Description: "cel.bind - Nested bindings",
					Expression:  `cel.bind(a, "hello", cel.bind(b, "world", a + b + b + a))`,
				},
				{
					Description: "cel.bind - Avoid list allocation in exists comprehension",
					Expression:  `cel.bind(valid_values, [1, 2, 3], [4, 5, 6].exists(elem, elem in valid_values))`,
				},
				{
					Description: "cel.bind - Reuse computed value multiple times",
					Expression:  `cel.bind(expensive_calc, size(large_list) * 100, expensive_calc + expensive_calc)`,
				},
				{
					Description: "cel.bind - Intermediate calculation for clarity",
					Expression:  `cel.bind(total, a + b + c, total > 100 ? total : 0)`,
				},
			},
			EnvOptions: []cel.EnvOption{celext.Bindings(celext.BindingsVersion(1))},
		},
		{
			Name:        "encoders",
			Links:       []string{"https://github.com/google/cel-go/blob/master/ext/encoders.go"},
			Description: "Encoding and decoding functions",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "Base64 encode a string",
					Expression:  `base64.encode("hello")`,
				},
				{
					Description: "Base64 decode a string",
					Expression:  `base64.decode("aGVsbG8=")`,
				},
			},
			EnvOptions: []cel.EnvOption{celext.Encoders(celext.EncodersVersion(1))},
		},
		{
			Name:        "math",
			Links:       []string{"https://github.com/google/cel-go/blob/master/ext/math.go"},
			Description: "Mathematical functions",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "math.least - Minimum of two numbers",
					Expression:  `math.least(5, 10)`,
				},
				{
					Description: "math.least - Minimum from list",
					Expression:  `math.least([-42.0, -21.5, -100.0])`,
				},
				{
					Description: "math.greatest - Maximum of two numbers",
					Expression:  `math.greatest(5, 10)`,
				},
				{
					Description: "math.greatest - Maximum from list",
					Expression:  `math.greatest([1, 2, 3, 4, 5])`,
				},
				{
					Description: "math.abs - Absolute value (v1)",
					Expression:  `math.abs(-5)`,
				},
				{
					Description: "math.ceil - Round up (v1)",
					Expression:  `math.ceil(1.2)`,
				},
				{
					Description: "math.floor - Round down (v1)",
					Expression:  `math.floor(1.8)`,
				},
				{
					Description: "math.round - Round to nearest (v1)",
					Expression:  `math.round(1.5)`,
				},
				{
					Description: "math.trunc - Truncate decimal (v1)",
					Expression:  `math.trunc(1.9)`,
				},
				{
					Description: "math.sign - Get sign (-1, 0, or 1) (v1)",
					Expression:  `math.sign(-42)`,
				},
				{
					Description: "math.isInf - Check if infinite (v1)",
					Expression:  `math.isInf(1.0/0.0)`,
				},
				{
					Description: "math.isNaN - Check if NaN (v1)",
					Expression:  `math.isNaN(0.0/0.0)`,
				},
				{
					Description: "math.isFinite - Check if finite number (v1)",
					Expression:  `math.isFinite(1.2)`,
				},
				{
					Description: "math.sqrt - Square root (v2)",
					Expression:  `math.sqrt(81)`,
				},
				{
					Description: "math.bitAnd - Bitwise AND (v1)",
					Expression:  `math.bitAnd(3, 5)`,
				},
				{
					Description: "math.bitOr - Bitwise OR (v1)",
					Expression:  `math.bitOr(1u, 2u)`,
				},
				{
					Description: "math.bitXor - Bitwise XOR (v1)",
					Expression:  `math.bitXor(3u, 5u)`,
				},
				{
					Description: "math.bitNot - Bitwise NOT (v1)",
					Expression:  `math.bitNot(1)`,
				},
				{
					Description: "math.bitShiftLeft - Left bit shift (v1)",
					Expression:  `math.bitShiftLeft(1, 2)`,
				},
				{
					Description: "math.bitShiftRight - Right bit shift (v1)",
					Expression:  `math.bitShiftRight(1024, 2)`,
				},
			},
			EnvOptions: []cel.EnvOption{celext.Math(celext.MathVersion(2))},
		},
		{
			Name:        "protos",
			Links:       []string{"https://github.com/google/cel-go/blob/master/ext/protos.go"},
			Description: "Protobuf related functions for proto2 extension fields",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "proto.hasExt - Check if extension field is set on proto2 message",
					Expression:  `proto.hasExt(msg, google.expr.proto2.test.int32_ext)`,
				},
				{
					Description: "proto.hasExt - Test for deprecated field option",
					Expression:  `proto.hasExt(msg, google.protobuf.FieldOptions.deprecated)`,
				},
				{
					Description: "proto.getExt - Retrieve extension field value (returns default if not set)",
					Expression:  `proto.getExt(msg, google.expr.proto2.test.int32_ext)`,
				},
				{
					Description: "proto.getExt - Get field option value with safe traversal",
					Expression:  `proto.getExt(msg, google.protobuf.FieldOptions.deprecated)`,
				},
				{
					Description: "proto.getExt - Access custom extension field",
					Expression:  `proto.getExt(msg, my.package.custom_extension)`,
				},
			},
			EnvOptions: []cel.EnvOption{celext.Protos(celext.ProtosVersion(1))},
		},
		{
			Name:        "sets",
			Links:       []string{"https://github.com/google/cel-go/blob/master/ext/sets.go"},
			Description: "Set manipulation functions",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "sets.contains - Empty subset is always contained",
					Expression:  `sets.contains([], [])`,
				},
				{
					Description: "sets.contains - Check if superset contains all elements",
					Expression:  `sets.contains([1, 2, 3, 4], [2, 3])`,
				},
				{
					Description: "sets.contains - Works with mixed numeric types",
					Expression:  `sets.contains([1, 2.0, 3u], [1.0, 2u, 3])`,
				},
				{
					Description: "sets.contains - False when subset has extra elements",
					Expression:  `sets.contains([], [1])`,
				},
				{
					Description: "sets.equivalent - Empty sets are equivalent",
					Expression:  `sets.equivalent([], [])`,
				},
				{
					Description: "sets.equivalent - Duplicates don't affect equivalence",
					Expression:  `sets.equivalent([1], [1, 1])`,
				},
				{
					Description: "sets.equivalent - Works with mixed numeric types",
					Expression:  `sets.equivalent([1], [1u, 1.0])`,
				},
				{
					Description: "sets.equivalent - Order doesn't matter",
					Expression:  `sets.equivalent([1, 2, 3], [3u, 2.0, 1])`,
				},
				{
					Description: "sets.intersects - Empty set never intersects",
					Expression:  `sets.intersects([1], [])`,
				},
				{
					Description: "sets.intersects - Check for any common element",
					Expression:  `sets.intersects([1, 2, 3], [2, 3, 4])`,
				},
				{
					Description: "sets.intersects - Works with nested structures",
					Expression:  `sets.intersects([[1], [2, 3]], [[1, 2], [2, 3.0]])`,
				},
			},
			EnvOptions: []cel.EnvOption{celext.Sets(celext.SetsVersion(1))},
		},
		{
			Name:        "homogeneousAggregateLiterals",
			Links:       []string{"https://github.com/google/cel-go/blob/e2bc9c90751b39e3b8401b6394e5f4dab2d48808/cel/options.go#L179-L186"},
			Description: "HomogeneousAggregateLiterals disables mixed type list and map literal values. Note: heterogeneous aggregates are still possible when provided as variables, via conversion of well-known dynamic types, or with unchecked expressions",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "Valid homogeneous list of integers",
					Expression:  `[1, 2, 3, 4, 5]`,
				},
				{
					Description: "Valid homogeneous list of strings",
					Expression:  `["apple", "banana", "cherry"]`,
				},
				{
					Description: "Valid homogeneous list of doubles",
					Expression:  `[1.0, 2.5, 3.14]`,
				},
				{
					Description: "Valid homogeneous map with string keys and int values",
					Expression:  `{"a": 1, "b": 2, "c": 3}`,
				},
				{
					Description: "Valid homogeneous map with string keys and string values",
					Expression:  `{"name": "John", "city": "NYC", "country": "USA"}`,
				},
				{
					Description: "Valid nested homogeneous lists",
					Expression:  `[[1, 2], [3, 4], [5, 6]]`,
				},
				{
					Description: "Invalid: mixed integer and string types (would fail with this option)",
					Expression:  `// [1, "two", 3] would be rejected at type-check time`,
				},
				{
					Description: "Invalid: mixed numeric types in list (would fail with this option)",
					Expression:  `// [1, 2.0, 3] would be rejected (int and double mixed)`,
				},
				{
					Description: "Invalid: mixed value types in map (would fail with this option)",
					Expression:  `// {"a": 1, "b": "two"} would be rejected`,
				},
				{
					Description: "Note: heterogeneous aggregates are still allowed when from variables",
					Expression:  `// Variables containing mixed types are not affected by this validation`,
				},
			},
			EnvOptions: []cel.EnvOption{cel.HomogeneousAggregateLiterals()},
		},
		{
			Name:        "eagerlyValidateDeclarations",
			Links:       []string{"https://github.com/google/cel-go/blob/e2bc9c90751b39e3b8401b6394e5f4dab2d48808/cel/options.go#L167C4-L177"},
			Description: "EagerlyValidateDeclarations ensures that any collisions between configured declarations are caught at the time of the `NewEnv` call. This is useful for bootstrapping a base cel.Env value. Calls to base Env.Extend() will be significantly faster when declarations are eagerly validated as declarations will be collision-checked at most once and only incrementally by way of Extend. Disabled by default as not all environments are used for type-checking.",
			Custom:      false,
			Examples:    []celexp.Example{},
			EnvOptions:  []cel.EnvOption{cel.EagerlyValidateDeclarations(true)},
		},
		{
			Name:        "defaultUTCTimeZone",
			Links:       []string{"https://github.com/google/cel-go/blob/e2bc9c90751b39e3b8401b6394e5f4dab2d48808/cel/options.go#L836-L840"},
			Description: "DefaultUTCTimeZone ensures that time-based operations use the UTC timezone rather than the input time's local timezone",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "Time operations use UTC instead of local time",
					Expression:  `timestamp("2025-12-10T15:30:00Z").getHours()`,
				},
			},
			EnvOptions: []cel.EnvOption{cel.DefaultUTCTimeZone(true)},
		},
		{
			Name:        "crossTypeNumericComparisons",
			Links:       []string{"https://github.com/google/cel-go/blob/e2bc9c90751b39e3b8401b6394e5f4dab2d48808/cel/options.go#L831-L834"},
			Description: "CrossTypeNumericComparisons makes it possible to compare across numeric types, e.g. double < int",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "Compare double with int",
					Expression:  `3.14 > 3`,
				},
				{
					Description: "Compare int with uint",
					Expression:  `5 < 10u`,
				},
			},
			EnvOptions: []cel.EnvOption{cel.CrossTypeNumericComparisons(true)},
		},
		{
			Name:        "optionalTypes",
			Links:       []string{"https://github.com/google/cel-go/blob/e2bc9c90751b39e3b8401b6394e5f4dab2d48808/cel/library.go#L207-L368"},
			Description: "OptionalTypes enable support for optional syntax and types in CEL. The optional value type makes it possible to express whether variables have been provided, whether a result has been computed, and in the future whether an object field path, map key value, or list index has a value",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "optional.of - Create optional with value",
					Expression:  `optional.of(1)`,
				},
				{
					Description: "optional.ofNonZeroValue - Create optional, only if value is non-zero",
					Expression:  `optional.ofNonZeroValue("hello")`,
				},
				{
					Description: "optional.ofNonZeroValue - Returns optional.none() for empty values",
					Expression:  `optional.ofNonZeroValue("")`,
				},
				{
					Description: "optional.none - Create empty optional",
					Expression:  `optional.none()`,
				},
				{
					Description: "hasValue - Check if optional contains a value",
					Expression:  `optional.of({1: 2}).hasValue()`,
				},
				{
					Description: "value - Extract value from optional (error if none)",
					Expression:  `optional.of(1).value()`,
				},
				{
					Description: "Optional field selection - Returns optional of field if present",
					Expression:  `msg.?field`,
				},
				{
					Description: "Optional field chaining - Viral optional selection",
					Expression:  `msg.?field.?nested_field`,
				},
				{
					Description: "Optional list indexing - Element value or optional.none()",
					Expression:  `[1, 2, 3][?x]`,
				},
				{
					Description: "Optional map indexing - Value at key or optional.none()",
					Expression:  `map_value[?key]`,
				},
				{
					Description: "or - Chain optionals, return first with value",
					Expression:  `optional.none().or(optional.of(1))`,
				},
				{
					Description: "or - Chain optional list access",
					Expression:  `[1, 2, 3][?x].or([3, 4, 5][?y])`,
				},
				{
					Description: "orValue - Return value or default",
					Expression:  `{'hello': 'world'}[?greeting].orValue('you')`,
				},
				{
					Description: "optMap - Transform optional value (v0)",
					Expression:  `request.auth.tokens.?sub.optMap(id, 'dev.cel.' + id)`,
				},
				{
					Description: "optMap - Returns optional.none() if input is none",
					Expression:  `optional.none().optMap(i, i * 2)`,
				},
				{
					Description: "optFlatMap - Transform to optional result (v1)",
					Expression:  `m.?key.optFlatMap(k, k.?subkey)`,
				},
				{
					Description: "Optional field setting in map - Conditionally set field",
					Expression:  `{?key: obj.?field.subfield}`,
				},
				{
					Description: "Optional element setting in list - Conditionally add elements",
					Expression:  `[a, ?b, ?c]`,
				},
				{
					Description: "first - Get first list element as optional (v2)",
					Expression:  `[1, 2, 3].first()`,
				},
				{
					Description: "last - Get last list element as optional (v2)",
					Expression:  `[1, 2, 3].last()`,
				},
				{
					Description: "optional.unwrap - Filter out none values from list (v2)",
					Expression:  `optional.unwrap([optional.of(42), optional.none()])`,
				},
				{
					Description: "unwrapOpt - Filter out none values (postfix notation) (v2)",
					Expression:  `[optional.of(42), optional.none()].unwrapOpt()`,
				},
			},
			EnvOptions: []cel.EnvOption{cel.OptionalTypes()},
		},
		{
			Name:        "astValidators",
			Links:       []string{"https://github.com/google/cel-go/blob/master/cel/validator.go"},
			Description: "ASTValidators enable various AST validation options. The available validators are: ValidateDurationLiterals, ValidateTimestampLiterals, ValidateRegexLiterals, ValidateHomogeneousAggregateLiterals",
			Custom:      false,
			Examples: []celexp.Example{
				{
					Description: "Validates duration literal format",
					Expression:  `duration("1h30m")`,
				},
				{
					Description: "Validates timestamp literal format",
					Expression:  `timestamp("2025-12-10T15:30:00Z")`,
				},
				{
					Description: "Validates regex literal syntax",
					Expression:  `"test".matches("^[a-z]+$")`,
				},
			},
			EnvOptions: []cel.EnvOption{cel.ASTValidators(
				cel.ValidateDurationLiterals(),
				cel.ValidateTimestampLiterals(),
				cel.ValidateRegexLiterals(),
				cel.ValidateHomogeneousAggregateLiterals(),
			)},
		},
	}
	return funcs
}

// SetFunctionNames populates the FunctionNames field for each ExtFunction in the list
// by creating a CEL environment with the EnvOptions and extracting the function names
// from the environment's function declarations. It compares the functions in the
// environment with the EnvOptions against a baseline environment to identify which
// functions are added by the specific extension.
//
// Example usage:
//
//	funcs := ext.GetCelExtFunctions()
//	err := ext.SetFunctionNames(funcs)
//	if err != nil {
//	    return err
//	}
//	// Now funcs[i].FunctionNames will be populated with the function names
//	// for each extension, e.g., ["charAt", "indexOf", "replace", ...] for strings
func SetFunctionNames(funcs celexp.ExtFunctionList) error {
	// Create a base environment to get baseline functions
	baseEnv, err := cel.NewEnv()
	if err != nil {
		return err
	}
	baseFuncs := baseEnv.Functions()

	for i := range funcs {
		fn := &funcs[i]
		if len(fn.EnvOptions) == 0 {
			continue
		}

		// Create an environment with the specific EnvOptions
		extEnv, err := cel.NewEnv(fn.EnvOptions...)
		if err != nil {
			return err
		}

		// Get function declarations from the extended environment
		extFuncs := extEnv.Functions()

		// Find functions that are in extEnv but not in baseEnv
		funcNames := make([]string, 0)
		for name := range extFuncs {
			if _, exists := baseFuncs[name]; !exists {
				funcNames = append(funcNames, name)
			}
		}

		// Sort for consistent output
		if len(funcNames) > 1 {
			for j := 0; j < len(funcNames)-1; j++ {
				for k := j + 1; k < len(funcNames); k++ {
					if funcNames[j] > funcNames[k] {
						funcNames[j], funcNames[k] = funcNames[k], funcNames[j]
					}
				}
			}
		}

		fn.FunctionNames = funcNames
	}
	return nil
}

// Custom returns a list of custom CEL extension functions implemented in this
// project. These are functions with Custom=true that extend CEL with
// project-specific functionality like arrays, strings, filepath, guid, map,
// marshalling, debugging, sorting, and time operations.
//
// Example usage:
//
//	funcs := ext.Custom()
//	for _, f := range funcs {
//	    fmt.Printf("Custom Function: %s (%s)\n", f.Name, f.Description)
//	}
func Custom() celexp.ExtFunctionList {
	return celexp.ExtFunctionList{
		// Arrays functions
		arrays.StringAddFunc(),
		arrays.StringsUniqueFunc(),

		// Debug functions
		debug.DebugThrowFunc(),
		debug.DebugSleepFunc(),

		// Filepath functions
		filepath.DirFunc(),
		filepath.NormalizeFunc(),
		filepath.ExistsFunc(),
		filepath.JoinFunc(),

		// GUID functions
		guid.NewFunc(),

		// Map functions
		celmap.AddFunc(),
		celmap.AddFailIfExistsFunc(),
		celmap.AddIfMissingFunc(),
		celmap.SelectFunc(),
		celmap.OmitFunc(),
		celmap.MergeFunc(),
		celmap.RecurseFunc(),

		// Marshalling functions
		marshalling.JSONMarshalFunc(),
		marshalling.JSONMarshalPrettyFunc(),
		marshalling.JSONUnmarshalFunc(),
		marshalling.YamlMarshalFunc(),
		marshalling.YamlUnmarshalFunc(),

		// Out functions
		out.NilFunc(),

		// Sort functions
		celsort.ObjectsFunc(),

		// String functions
		celstrings.CleanFunc(),
		celstrings.TitleFunc(),

		// Time functions
		celtime.NowFunc(),
		celtime.NowFmtFunc(),
	}
}

// All returns a combined list of all CEL extension functions, including both
// built-in extensions from google/cel-go and custom extensions implemented in
// this project.
//
// Example usage:
//
//	funcs := ext.All()
//	err := ext.SetFunctionNames(funcs)
//	if err != nil {
//	    return err
//	}
//	// Now you have all functions with their FunctionNames populated
func All() celexp.ExtFunctionList {
	builtIn := BuiltIn()
	custom := Custom()

	// Combine both lists
	all := make(celexp.ExtFunctionList, 0, len(builtIn)+len(custom))
	all = append(all, builtIn...)
	all = append(all, custom...)

	return all
}
