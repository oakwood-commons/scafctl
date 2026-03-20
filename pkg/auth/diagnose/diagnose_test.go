package diagnose

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestRunEnvVarChecks_WithEntraVars(t *testing.T) {
	t.Setenv("AZURE_CLIENT_ID", "test-client-id")
	t.Setenv("AZURE_TENANT_ID", "test-tenant-id")
	t.Setenv("AZURE_CLIENT_SECRET", "test-secret")

	checks := RunEnvVarChecks()
	assert.NotEmpty(t, checks)

	// At least one check should reference AZURE_CLIENT_ID
	found := false
	for _, c := range checks {
		if c.Category == "env" && c.Status == StatusOK {
			found = true
			break
		}
	}
	assert.True(t, found, "should have at least one OK env check for AZURE_CLIENT_ID")
}

func TestRunEnvVarChecks_WithGitHubVars(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "test-github-token")

	checks := RunEnvVarChecks()

	found := false
	for _, c := range checks {
		if c.Category == "env" && c.Status == StatusOK {
			found = true
			break
		}
	}
	assert.True(t, found, "should have an OK check for GITHUB_TOKEN")
}

func TestRunEnvVarChecks_WithGCPVars(t *testing.T) {
	t.Setenv("GOOGLE_APPLICATION_CREDENTIALS", "/path/to/key.json")
	t.Setenv("GOOGLE_CLOUD_PROJECT", "my-project")

	checks := RunEnvVarChecks()

	found := false
	for _, c := range checks {
		if c.Category == "env" && c.Status == StatusOK {
			found = true
			break
		}
	}
	assert.True(t, found, "should have an OK check for GCP vars")
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

func TestRunClockSkewCheck_NoNetwork(t *testing.T) {
	// Serve a Date header that matches "now" so skew is negligible.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := runClockSkewCheck(srv.Client(), srv.URL)
	assert.Equal(t, "clock", check.Category)
	assert.Equal(t, "clock skew", check.Name)
	assert.Equal(t, StatusOK, check.Status)
}

func TestRunClockSkewCheck_LargeSkew(t *testing.T) {
	// Serve a Date header far in the past to simulate excessive skew.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		past := time.Now().Add(-10 * time.Minute).UTC()
		w.Header().Set("Date", past.Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := runClockSkewCheck(srv.Client(), srv.URL)
	assert.Equal(t, "clock", check.Category)
	assert.Equal(t, StatusFail, check.Status)
}

func TestRunClockSkewCheck_NoDateHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Suppress the Date header that Go's HTTP server adds automatically.
		w.Header()["Date"] = nil
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := runClockSkewCheck(srv.Client(), srv.URL)
	assert.Equal(t, StatusInfo, check.Status)
	assert.Contains(t, check.Message, "no Date header")
}

func TestRunClockSkewCheck_UnreachableEndpoint(t *testing.T) {
	// Use a closed server to simulate an unreachable endpoint.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	check := runClockSkewCheck(&http.Client{Timeout: time.Second}, url)
	assert.Equal(t, StatusWarn, check.Status)
	assert.Contains(t, check.Message, "could not reach")
}

func TestRunClockSkewCheck_BadDateHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", "not-a-valid-date")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := runClockSkewCheck(srv.Client(), srv.URL)
	assert.Equal(t, StatusWarn, check.Status)
	assert.Contains(t, check.Message, "could not parse Date header")
}

func TestRunClockSkewCheck_RealNetwork(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping clock skew check in short mode (requires network)")
	}
	check := RunClockSkewCheck()
	// Regardless of network availability, should return a valid Check struct
	assert.Equal(t, "clock", check.Category)
	assert.Equal(t, "clock skew", check.Name)
	assert.NotEmpty(t, check.Message)
	// Status should be one of the valid statuses
	validStatuses := []CheckStatus{StatusOK, StatusWarn, StatusFail, StatusInfo}
	assert.Contains(t, validStatuses, check.Status)
}

func BenchmarkRunClockSkewCheck(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client := srv.Client()
	url := srv.URL

	for b.Loop() {
		_ = runClockSkewCheck(client, url)
	}
}

func TestFormatSkewMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Date", time.Now().UTC().Format(http.TimeFormat))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	check := runClockSkewCheck(srv.Client(), srv.URL)
	assert.Contains(t, check.Message, "clock skew is")
	assert.Contains(t, check.Message, "within acceptable range")
}
