package provider

import (
	"fmt"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/oakwood-commons/scafctl/pkg/provider/schemahelper"
)

func BenchmarkRegistry_Register(b *testing.B) {
	providers := make([]Provider, b.N)
	for i := 0; i < b.N; i++ {
		providers[i] = newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewRegistry()
		_ = r.Register(providers[i])
	}
}

func BenchmarkRegistry_RegisterWithValidation(b *testing.B) {
	r := NewRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0")
		_ = r.Register(p)
	}
}

func BenchmarkRegistry_Get(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 100; i++ {
		_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("provider-%d", i%100)
		_, _ = r.Get(name)
	}
}

func BenchmarkRegistry_Has(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 100; i++ {
		_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("provider-%d", i%100)
		_ = r.Has(name)
	}
}

func BenchmarkRegistry_List(b *testing.B) {
	sizes := []int{10, 50, 100, 500}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			r := NewRegistry()
			for i := 0; i < size; i++ {
				_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0"))
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = r.List()
			}
		})
	}
}

func BenchmarkRegistry_ListProviders(b *testing.B) {
	sizes := []int{10, 50, 100, 500}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			r := NewRegistry()
			for i := 0; i < size; i++ {
				_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0"))
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = r.ListProviders()
			}
		})
	}
}

func BenchmarkRegistry_ListByCapability(b *testing.B) {
	r := NewRegistry()

	// Register 100 providers with mixed capabilities
	for i := 0; i < 100; i++ {
		var caps []Capability
		switch i % 3 {
		case 0:
			caps = []Capability{CapabilityFrom}
		case 1:
			caps = []Capability{CapabilityTransform}
		case 2:
			caps = []Capability{CapabilityAction}
		}
		_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0", caps...))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.ListByCapability(CapabilityFrom)
	}
}

func BenchmarkRegistry_ListByCategory(b *testing.B) {
	r := NewRegistry()

	// Register 100 providers with mixed categories
	categories := []string{"api", "database", "filesystem", "network"}
	for i := 0; i < 100; i++ {
		p := newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0")
		p.Descriptor().Category = categories[i%len(categories)]
		_ = r.Register(p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.ListByCategory("api")
	}
}

func BenchmarkRegistry_Count(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 100; i++ {
		_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.Count()
	}
}

func BenchmarkRegistry_ConcurrentReads(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 100; i++ {
		_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0"))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			name := fmt.Sprintf("provider-%d", i%100)
			_, _ = r.Get(name)
			i++
		}
	})
}

func BenchmarkRegistry_ConcurrentWrites(b *testing.B) {
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			r := NewRegistry()
			p := newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0")
			_ = r.Register(p)
			i++
		}
	})
}

func BenchmarkRegistry_MixedOperations(b *testing.B) {
	r := NewRegistry()
	for i := 0; i < 50; i++ {
		_ = r.Register(newMockProvider(fmt.Sprintf("provider-%d", i), "1.0.0"))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			switch i % 4 {
			case 0:
				name := fmt.Sprintf("provider-%d", i%50)
				_, _ = r.Get(name)
			case 1:
				_ = r.List()
			case 2:
				_ = r.Has(fmt.Sprintf("provider-%d", i%50))
			case 3:
				_ = r.ListByCapability(CapabilityFrom)
			}
			i++
		}
	})
}

func BenchmarkGlobalRegistry_Register(b *testing.B) {
	ResetGlobalRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := newMockProvider(fmt.Sprintf("global-provider-%d", i), "1.0.0")
		_ = Register(p)
	}

	b.StopTimer()
	ResetGlobalRegistry()
}

func BenchmarkGlobalRegistry_Get(b *testing.B) {
	ResetGlobalRegistry()

	for i := 0; i < 100; i++ {
		_ = Register(newMockProvider(fmt.Sprintf("global-provider-%d", i), "1.0.0"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("global-provider-%d", i%100)
		_, _ = Get(name)
	}

	b.StopTimer()
	ResetGlobalRegistry()
}

func BenchmarkValidateDescriptor(b *testing.B) {
	r := NewRegistry()
	desc := &Descriptor{
		Name:         "test",
		Version:      MustParseVersion("1.0.0"),
		Capabilities: []Capability{CapabilityFrom, CapabilityTransform},
		Schema: schemahelper.ObjectSchema([]string{"input1"}, map[string]*jsonschema.Schema{
			"input1": schemahelper.StringProp(""),
			"input2": schemahelper.IntProp(""),
			"input3": schemahelper.BoolProp(""),
		}),
		OutputSchemas: map[Capability]*jsonschema.Schema{
			CapabilityFrom: schemahelper.ObjectSchema(nil, map[string]*jsonschema.Schema{
				"output": schemahelper.StringProp(""),
			}),
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = r.validateDescriptor(desc)
	}
}

// Helper function for benchmarks
func MustParseVersion(v string) *semver.Version {
	ver, err := semver.NewVersion(v)
	if err != nil {
		panic(err)
	}
	return ver
}
