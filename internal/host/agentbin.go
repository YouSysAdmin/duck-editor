package host

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/update"
)

// errNoArchive reports a release without the archive asked for.
var errNoArchive = update.ErrNoArchive

// AgentBinary finds the dk binary to install on a remote system. A
// development build uses dk-<os>-<arch> in cacheDir, built by task agent.
// A release uses its static Linux agent, dk-agent-linux-<arch>, which runs
// on any C library, or else the full dk of that system: this executable
// or the release archive. With download set a missing archive is
// downloaded into cacheDir.
func AgentBinary(cacheDir, version, goos, goarch string, download bool) (string, error) {
	self := goos == runtime.GOOS && goarch == runtime.GOARCH
	if self && (goos != "linux" || version == "dev") {
		return os.Executable()
	}

	if version == "dev" {
		cached := filepath.Join(cacheDir, "dk-"+goos+"-"+goarch)
		if _, err := os.Stat(cached); err != nil {
			return "", fmt.Errorf("no dk binary for %s/%s, a development build looks for %s, task agent builds it", goos, goarch, cached)
		}

		return cached, nil
	}

	dir := filepath.Join(cacheDir, version)
	full := filepath.Join(dir, "dk-"+goos+"-"+goarch)
	agent := full
	if goos == "linux" {
		agent = filepath.Join(dir, "dk-agent-"+goos+"-"+goarch)
	}

	if _, err := os.Stat(agent); err == nil {
		return agent, nil
	}

	if !download {
		return "", fmt.Errorf("no dk binary for %s/%s yet", goos, goarch)
	}

	missing := false
	if agent != full {
		err := downloadRelease(version, filepath.Base(agent), agent)
		missing = errors.Is(err, errNoArchive)
		switch {
		case err == nil:
			return agent, nil
		case !errors.Is(err, errNoArchive) && !self:
			return "", fmt.Errorf("download the dk agent %s for %s/%s: %w", version, goos, goarch, err)
		}
	}

	// A release before the static agents uses the full dk, linked as the
	// agent so the next connection finds it without asking GitHub again.
	src := full
	if self {
		exe, err := os.Executable()
		if err != nil {
			return "", err
		}

		src = exe
	} else if _, err := os.Stat(full); err != nil {
		if err := downloadRelease(version, filepath.Base(full), full); err != nil {
			return "", fmt.Errorf("download dk %s for %s/%s: %w", version, goos, goarch, err)
		}
	}

	if missing {
		_ = os.Symlink(src, agent)
	}

	return src, nil
}

// downloadRelease fetches the release archive name.tar.gz, checks it
// against the published checksums and extracts the binary to dest.
func downloadRelease(version, name, dest string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	data, err := update.Download(ctx, version, name)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}

	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return err
	}

	return os.Rename(tmp, dest)
}
