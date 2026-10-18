package vcs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/YouSysAdmin/duck-editor/internal/host"
)

// BlameLine says who last changed a line.
type BlameLine struct {
	// Commit is the full hash, empty for a line not committed yet.
	Commit     string
	Author     string
	AuthorMail string
	Time       time.Time
	Summary    string
}

// Committed reports whether the line comes from a commit.
func (b BlameLine) Committed() bool {
	return b.Commit != ""
}

// Blame returns who last changed line, zero based, of the file at path.
// content stands in for the file on disk, so unsaved edits are blamed
// as not committed and the line numbers match the buffer.
func Blame(ctx context.Context, h host.Host, path, content string, line int) (BlameLine, error) {
	n := strconv.Itoa(line + 1)
	stdout, _, err := h.Run(ctx, host.Command{
		Name:  "git",
		Args:  []string{"-C", filepath.Dir(path), "blame", "--porcelain", "-L", n + "," + n, "--contents", "-", "--", filepath.Base(path)},
		Stdin: []byte(content),
	})
	if err != nil {
		var exitErr *host.ExitError
		if errors.As(err, &exitErr) {
			return BlameLine{}, fmt.Errorf("git blame: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}

		return BlameLine{}, fmt.Errorf("git blame: %w", err)
	}

	return parseBlame(string(stdout))
}

// parseBlame reads one entry of git blame --porcelain output.
func parseBlame(out string) (BlameLine, error) {
	var b BlameLine
	scanner := bufio.NewScanner(strings.NewReader(out))
	if !scanner.Scan() {
		return b, errors.New("git blame: empty output")
	}

	hash, _, _ := strings.Cut(scanner.Text(), " ")
	if strings.Trim(hash, "0") != "" {
		b.Commit = hash
	}

	for scanner.Scan() {
		key, value, _ := strings.Cut(scanner.Text(), " ")
		switch key {
		case "author":
			b.Author = value
		case "author-mail":
			b.AuthorMail = strings.Trim(value, "<>")
		case "author-time":
			if sec, err := strconv.ParseInt(value, 10, 64); err == nil {
				b.Time = time.Unix(sec, 0)
			}
		case "summary":
			b.Summary = value
		}
	}

	return b, nil
}

// RelativeTime describes how long ago t was in words, such as
// "3 days ago".
func RelativeTime(t, now time.Time) string {
	d := now.Sub(t)
	units := []struct {
		size time.Duration
		name string
	}{
		{365 * 24 * time.Hour, "year"},
		{30 * 24 * time.Hour, "month"},
		{7 * 24 * time.Hour, "week"},
		{24 * time.Hour, "day"},
		{time.Hour, "hour"},
		{time.Minute, "minute"},
	}
	for _, u := range units {
		if n := int(d / u.size); n >= 1 {
			if n == 1 {
				return "1 " + u.name + " ago"
			}

			return strconv.Itoa(n) + " " + u.name + "s ago"
		}
	}

	return "just now"
}
