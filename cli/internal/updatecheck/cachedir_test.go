package updatecheck

import (
	"path/filepath"
	"testing"
)

func TestDefaultDir_EnvOverrideUsedVerbatim(t *testing.T) {
	want := t.TempDir()
	t.Setenv(EnvCacheDirOverride, want)
	got, err := DefaultDir()
	if err != nil {
		t.Fatalf("DefaultDir: %v", err)
	}
	if got != want {
		t.Errorf("DefaultDir = %q, want %q (env override should be verbatim, no /gaffer suffix)", got, want)
	}
}

func TestDefaultDir_FallsBackToUserCacheDir(t *testing.T) {
	t.Setenv(EnvCacheDirOverride, "")
	got, err := DefaultDir()
	if err != nil {
		t.Skipf("os.UserCacheDir() failed in this environment: %v", err)
	}
	if filepath.Base(got) != "gaffer" {
		t.Errorf("DefaultDir = %q, want path ending in /gaffer", got)
	}
}
