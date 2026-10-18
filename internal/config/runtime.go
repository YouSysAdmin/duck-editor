package config

import (
	"os"
	"path/filepath"
)

// RuntimeDirs lists the runtime directories from highest to lowest
// priority: the config dir runtime and the runtime next to the
// executable.
func RuntimeDirs() []string {
	dirs := []string{filepath.Join(Dir(), "runtime")}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}

		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "runtime"))
	}

	return dirs
}

// RuntimeFile finds a file relative to the runtime directories.
func RuntimeFile(rel string) (string, bool) {
	for _, dir := range RuntimeDirs() {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err == nil {
			return p, true
		}
	}

	return "", false
}

// ThemeDirs lists the base directories searched for themes: the config
// dir first, then the runtime dirs.
func ThemeDirs() []string {
	return append([]string{Dir()}, RuntimeDirs()...)
}

// CacheDir is ~/.cache/duck, or the duck directory under XDG_CACHE_HOME.
func CacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(os.TempDir(), "duck")
		}

		base = filepath.Join(home, ".cache")
	}

	return filepath.Join(base, "duck")
}

// RecoveryDir receives unsaved buffers when the editor crashes.
func RecoveryDir() string {
	return filepath.Join(CacheDir(), "recovery")
}

// LogPath is the editor log file under the cache directory.
func LogPath() string {
	return filepath.Join(CacheDir(), "duck.log")
}

// GrammarCacheDir receives the grammar libraries extracted from the
// binary.
func GrammarCacheDir() string {
	return filepath.Join(CacheDir(), "grammars")
}

// LanguagesFiles lists the languages.yaml files to overlay on the
// builtin one: the user file, then the workspace one.
func LanguagesFiles() []string {
	paths := []string{filepath.Join(Dir(), "languages.yaml")}
	if ws := WorkspaceDir(); ws != "" {
		paths = append(paths, filepath.Join(ws, "languages.yaml"))
	}

	return paths
}
