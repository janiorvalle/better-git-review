package xdg

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestUnixPathsPreserveXDGAndLegacyDefaults(t *testing.T) {
	getenv := func(name string) string {
		switch name {
		case "XDG_CONFIG_HOME":
			return "/xdg/config"
		case "XDG_STATE_HOME":
			return "/xdg/state"
		default:
			return ""
		}
	}
	config, err := configHome("linux", getenv, func() (string, error) {
		return "/home/reviewer", nil
	}, func() (string, error) {
		return "", errors.New("unexpected UserConfigDir call")
	})
	if err != nil || config != "/xdg/config" {
		t.Fatalf("config = %q, %v", config, err)
	}
	state, err := stateDir("linux", getenv, func() (string, error) {
		return "/home/reviewer", nil
	}, func() (string, error) {
		return "", errors.New("unexpected UserCacheDir call")
	})
	if err != nil || state != filepath.Join("/xdg/state", appName) {
		t.Fatalf("state = %q, %v", state, err)
	}

	emptyEnv := func(string) string { return "" }
	config, err = configHome("darwin", emptyEnv, func() (string, error) {
		return "/Users/reviewer", nil
	}, nil)
	if err != nil || config != filepath.Join("/Users/reviewer", ".config") {
		t.Fatalf("legacy config = %q, %v", config, err)
	}
	state, err = stateDir("darwin", emptyEnv, func() (string, error) {
		return "/Users/reviewer", nil
	}, nil)
	if err != nil || state != filepath.Join("/Users/reviewer", ".local", "state", appName) {
		t.Fatalf("legacy state = %q, %v", state, err)
	}
}

func TestWindowsPathsUseNativeConfigAndCacheDirectories(t *testing.T) {
	config, err := configHome("windows", func(string) string {
		return `C:\ignored`
	}, nil, func() (string, error) {
		return `C:\Users\reviewer\AppData\Roaming`, nil
	})
	if err != nil || config != `C:\Users\reviewer\AppData\Roaming` {
		t.Fatalf("config = %q, %v", config, err)
	}
	state, err := stateDir("windows", func(string) string {
		return `C:\ignored`
	}, nil, func() (string, error) {
		return `C:\Users\reviewer\AppData\Local`, nil
	})
	want := filepath.Join(`C:\Users\reviewer\AppData\Local`, appName)
	if err != nil || state != want {
		t.Fatalf("state = %q, want %q, err %v", state, want, err)
	}
}
