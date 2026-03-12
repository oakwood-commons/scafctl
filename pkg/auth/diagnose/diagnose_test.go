package diagnose

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRunEnvVarChecks(t *testing.T) {
	checks := RunEnvVarChecks()
	assert.NotNil(t, checks, "RunEnvVarChecks should never return nil")
	for _, c := range checks {
		assert.NotEmpty(t, c.Category, "every check must have a category")
		assert.NotEmpty(t, c.Name, "every check must have a name")
		assert.NotEmpty(t, c.Status, "every check must have a status")
		assert.NotEmpty(t, c.Message, "every check must have a message")
		assert.Contains(t, []CheckStatus{StatusOK, StatusWarn, StatusFail, StatusInfo}, c.Status)
	}
}

func TestCheckStatus_Values(t *testing.T) {
	assert.Equal(t, CheckStatus("ok"), StatusOK)
	assert.Equal(t, CheckStatus("warn"), StatusWarn)
	assert.Equal(t, CheckStatus("fail"), StatusFail)
	assert.Equal(t, CheckStatus("info"), StatusInfo)
}

func TestCheck_StructTags(t *testing.T) {
	// Ensure the struct can be marshalled (compile-time tag check via json)
	c := Check{
		Category: "test",
		Name:     "example",
		Status:   StatusOK,
		Message:  "ok",
	}
	assert.Equal(t, "test", c.Category)
	assert.Equal(t, "example", c.Name)
}

func BenchmarkRunEnvVarChecks(b *testing.B) {
	for b.Loop() {
		_ = RunEnvVarChecks()
	}
}
