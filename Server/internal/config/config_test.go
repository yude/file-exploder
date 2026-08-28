package config

import (
	"testing"
	"time"
)

func TestDefaultConfigUsesTheDefaultJobTimeoutWithoutTheEnvVar(t *testing.T) {
	t.Setenv("FILE_EXPLODER_JOB_TIMEOUT", "")
	cfg := DefaultConfig()
	if cfg.JobTimeout != DefaultJobTimeout {
		t.Fatalf("JobTimeout = %v, want the default %v", cfg.JobTimeout, DefaultJobTimeout)
	}
}

func TestDefaultConfigHonoursTheJobTimeoutEnvVar(t *testing.T) {
	t.Setenv("FILE_EXPLODER_JOB_TIMEOUT", "45m")
	cfg := DefaultConfig()
	if cfg.JobTimeout != 45*time.Minute {
		t.Fatalf("JobTimeout = %v, want 45m", cfg.JobTimeout)
	}
}

func TestDefaultConfigIgnoresAnInvalidJobTimeout(t *testing.T) {
	for _, value := range []string{"not-a-duration", "-10m", "0s"} {
		t.Setenv("FILE_EXPLODER_JOB_TIMEOUT", value)
		cfg := DefaultConfig()
		if cfg.JobTimeout != DefaultJobTimeout {
			t.Fatalf("FILE_EXPLODER_JOB_TIMEOUT=%q: JobTimeout = %v, want the default %v", value, cfg.JobTimeout, DefaultJobTimeout)
		}
	}
}
