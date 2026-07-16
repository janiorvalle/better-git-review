package xdg

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "better-git-review"

func ConfigHome() (string, error) {
	return configHome(runtime.GOOS, os.Getenv, os.UserHomeDir, os.UserConfigDir)
}

func StateDir() (string, error) {
	return stateDir(runtime.GOOS, os.Getenv, os.UserHomeDir, os.UserCacheDir)
}

func CacheDir() (string, error) {
	state, err := StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(state, "cache"), nil
}

func configHome(
	goos string,
	getenv func(string) string,
	userHomeDir func() (string, error),
	userConfigDir func() (string, error),
) (string, error) {
	if goos == "windows" {
		return userConfigDir()
	}
	if value := getenv("XDG_CONFIG_HOME"); value != "" {
		return value, nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

func stateDir(
	goos string,
	getenv func(string) string,
	userHomeDir func() (string, error),
	userCacheDir func() (string, error),
) (string, error) {
	if goos == "windows" {
		cacheHome, err := userCacheDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(cacheHome, appName), nil
	}
	if value := getenv("XDG_STATE_HOME"); value != "" {
		return filepath.Join(value, appName), nil
	}
	home, err := userHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", appName), nil
}
