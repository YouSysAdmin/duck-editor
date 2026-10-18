package host

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// releaseBase is where release archives are downloaded from, a variable
// so tests can serve them.
var releaseBase = "https://github.com/YouSysAdmin/duck-editor/releases/download"

// errNoArchive reports a release without the archive asked for.
var errNoArchive = errors.New("not in SHA256SUMS")

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

	base := releaseBase + "/v" + strings.TrimPrefix(version, "v") + "/"
	archive := name + ".tar.gz"
	sums, err := fetch(ctx, base+"SHA256SUMS")
	if err != nil {
		return err
	}

	want := ""
	for line := range strings.Lines(string(sums)) {
		if fields := strings.Fields(line); len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == archive {
			want = fields[0]
		}
	}

	if want == "" {
		return fmt.Errorf("%s: %w", archive, errNoArchive)
	}

	data, err := fetch(ctx, base+archive)
	if err != nil {
		return err
	}

	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != want {
		return fmt.Errorf("%s does not match its checksum", archive)
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return err
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%s holds no dk binary", archive)
		}

		if err != nil {
			return err
		}

		if hdr.Name != "dk" && hdr.Name != "./dk" {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}

		tmp := dest + ".tmp"
		f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}

		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()

			return err
		}

		if err := f.Close(); err != nil {
			return err
		}

		return os.Rename(tmp, dest)
	}
}

func fetch(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}

	return io.ReadAll(resp.Body)
}
