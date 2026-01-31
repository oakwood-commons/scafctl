package secrets

import (
	"context"
	"testing"
)

// BenchmarkEncrypt benchmarks the encryption function with various payload sizes.
func BenchmarkEncrypt(b *testing.B) {
	key, err := generateMasterKey()
	if err != nil {
		b.Fatal(err)
	}

	sizes := []struct {
		name string
		size int
	}{
		{"100B", 100},
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, sz := range sizes {
		data := make([]byte, sz.size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(sz.size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := encrypt(key, data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkDecrypt benchmarks the decryption function with various payload sizes.
func BenchmarkDecrypt(b *testing.B) {
	key, err := generateMasterKey()
	if err != nil {
		b.Fatal(err)
	}

	sizes := []struct {
		name string
		size int
	}{
		{"100B", 100},
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
		{"1MB", 1024 * 1024},
	}

	for _, sz := range sizes {
		data := make([]byte, sz.size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		encrypted, err := encrypt(key, data)
		if err != nil {
			b.Fatal(err)
		}

		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(sz.size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := decrypt(key, encrypted)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkEncryptDecrypt benchmarks the full round-trip.
func BenchmarkEncryptDecrypt(b *testing.B) {
	key, err := generateMasterKey()
	if err != nil {
		b.Fatal(err)
	}

	sizes := []struct {
		name string
		size int
	}{
		{"100B", 100},
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
	}

	for _, sz := range sizes {
		data := make([]byte, sz.size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		b.Run(sz.name, func(b *testing.B) {
			b.SetBytes(int64(sz.size) * 2) // Count both encrypt and decrypt
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				encrypted, err := encrypt(key, data)
				if err != nil {
					b.Fatal(err)
				}

				_, err = decrypt(key, encrypted)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkStoreSetGet benchmarks the Store.Set and Store.Get operations.
func BenchmarkStoreSetGet(b *testing.B) {
	tmpDir := b.TempDir()
	keyring := NewMockKeyring()

	store, err := New(
		WithSecretsDir(tmpDir),
		WithKeyring(keyring),
	)
	if err != nil {
		b.Fatal(err)
	}

	ctx := context.Background()

	sizes := []struct {
		name string
		size int
	}{
		{"100B", 100},
		{"1KB", 1024},
		{"10KB", 10 * 1024},
		{"100KB", 100 * 1024},
	}

	for _, sz := range sizes {
		data := make([]byte, sz.size)
		for i := range data {
			data[i] = byte(i % 256)
		}

		b.Run("Set_"+sz.name, func(b *testing.B) {
			b.SetBytes(int64(sz.size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				err := store.Set(ctx, "benchmark-secret", data)
				if err != nil {
					b.Fatal(err)
				}
			}
		})

		// Ensure secret exists for Get benchmark
		err := store.Set(ctx, "benchmark-secret", data)
		if err != nil {
			b.Fatal(err)
		}

		b.Run("Get_"+sz.name, func(b *testing.B) {
			b.SetBytes(int64(sz.size))
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, err := store.Get(ctx, "benchmark-secret")
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkValidateName benchmarks the name validation function.
func BenchmarkValidateName(b *testing.B) {
	names := []string{
		"a",
		"simple-name",
		"my.secret.v2",
		"API_KEY_PRODUCTION",
		"a1234567890123456789012345678901234567890123456789",
	}

	for _, name := range names {
		b.Run(name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = ValidateName(name)
			}
		})
	}
}

// BenchmarkGenerateMasterKey benchmarks master key generation.
func BenchmarkGenerateMasterKey(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_, err := generateMasterKey()
		if err != nil {
			b.Fatal(err)
		}
	}
}
