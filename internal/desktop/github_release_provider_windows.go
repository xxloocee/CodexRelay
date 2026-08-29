//go:build windows

package desktop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"golang.org/x/mod/semver"
)

const (
	gitHubWebBaseURL       = "https://github.com"
	gitHubAssetURLMetadata = "github-release.asset-url"
	maxChecksumFileSize    = 1 << 20
)

type gitHubReleaseProvider struct {
	repository    string
	checksumAsset string
	client        *http.Client
}

func newGitHubReleaseProvider(repository, checksumAsset string, client *http.Client) *gitHubReleaseProvider {
	return &gitHubReleaseProvider{
		repository:    strings.Trim(repository, "/"),
		checksumAsset: checksumAsset,
		client:        client,
	}
}

func (p *gitHubReleaseProvider) Name() string { return "github-release" }

func (p *gitHubReleaseProvider) Check(ctx context.Context, req updater.CheckRequest) (*updater.Release, error) {
	if !strings.EqualFold(req.Platform, "windows") {
		return nil, fmt.Errorf("github-release: unsupported platform %q", req.Platform)
	}
	arch := strings.ToLower(req.Arch)
	if arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("github-release: unsupported Windows architecture %q", req.Arch)
	}

	tag, err := p.latestTag(ctx)
	if err != nil {
		return nil, err
	}
	latestVersion, err := validSemver(tag)
	if err != nil {
		return nil, fmt.Errorf("github-release: invalid release tag %q: %w", tag, err)
	}
	currentVersion, err := validSemver(req.CurrentVersion)
	if err != nil {
		return nil, fmt.Errorf("github-release: invalid current version %q: %w", req.CurrentVersion, err)
	}
	if semver.Compare(latestVersion, currentVersion) <= 0 {
		return nil, nil
	}

	version := strings.TrimPrefix(latestVersion, "v")
	filename := fmt.Sprintf("CodexRelay-%s-%s.exe", version, arch)
	releaseBaseURL := fmt.Sprintf(
		"%s/%s/releases/download/%s",
		gitHubWebBaseURL,
		p.repository,
		url.PathEscape(tag),
	)
	assetURL := releaseBaseURL + "/" + url.PathEscape(filename)
	digest, err := p.fetchChecksum(ctx, releaseBaseURL+"/"+url.PathEscape(p.checksumAsset), filename)
	if err != nil {
		return nil, err
	}
	size, err := p.fetchArtifactSize(ctx, assetURL)
	if err != nil {
		return nil, err
	}

	return &updater.Release{
		Version: version,
		Channel: "stable",
		Name:    "CodexRelay " + tag,
		Artifact: updater.Artifact{
			Filename: filename,
			Filetype: "exe",
			Size:     size,
			Platform: "windows",
			Arch:     arch,
		},
		Verification: &updater.Verification{
			DigestAlgo: "sha256",
			Digest:     digest,
		},
		Metadata: map[string]any{
			gitHubAssetURLMetadata: assetURL,
		},
	}, nil
}

func (p *gitHubReleaseProvider) Download(
	ctx context.Context,
	release *updater.Release,
	dst io.Writer,
	onProgress func(written, total int64),
) error {
	if release == nil || release.Metadata == nil {
		return errors.New("github-release: release metadata is missing")
	}
	assetURL, ok := release.Metadata[gitHubAssetURLMetadata].(string)
	if !ok || assetURL == "" {
		return errors.New("github-release: release asset URL is missing")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/octet-stream")
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("github-release: download asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("github-release: download asset: HTTP %d", response.StatusCode)
	}

	total := release.Artifact.Size
	if total <= 0 && response.ContentLength > 0 {
		total = response.ContentLength
	}
	progress := &updateProgressWriter{dst: dst, total: total, onProgress: onProgress}
	if _, err := io.CopyBuffer(progress, response.Body, make([]byte, 64*1024)); err != nil {
		return fmt.Errorf("github-release: download asset: %w", err)
	}
	return nil
}

func (p *gitHubReleaseProvider) latestTag(ctx context.Context) (string, error) {
	latestURL := fmt.Sprintf("%s/%s/releases/latest", gitHubWebBaseURL, p.repository)
	// GitHub's /releases/latest endpoint redirects to the concrete tag. Some
	// HTTP/2 paths and intermediaries intermittently terminate redirected HEAD
	// requests with EOF, while the equivalent GET request is handled reliably.
	// The response body is not needed; we only use the final URL after the
	// redirect and close it below.
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", err
	}
	response, err := p.client.Do(request)
	if err != nil {
		return "", fmt.Errorf("github-release: resolve latest release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("github-release: resolve latest release: HTTP %d", response.StatusCode)
	}
	if !strings.EqualFold(response.Request.URL.Hostname(), "github.com") {
		return "", fmt.Errorf("github-release: unexpected release host %q", response.Request.URL.Hostname())
	}

	prefix := "/" + p.repository + "/releases/tag/"
	if !strings.HasPrefix(response.Request.URL.Path, prefix) {
		return "", fmt.Errorf("github-release: unexpected latest release URL %q", response.Request.URL.String())
	}
	tag, err := url.PathUnescape(strings.TrimPrefix(response.Request.URL.Path, prefix))
	if err != nil || tag == "" || strings.Contains(tag, "/") {
		return "", fmt.Errorf("github-release: invalid tag in latest release URL %q", response.Request.URL.String())
	}
	return tag, nil
}

func (p *gitHubReleaseProvider) fetchChecksum(ctx context.Context, checksumURL, filename string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "text/plain, application/octet-stream")
	response, err := p.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("github-release: download checksum file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("github-release: download checksum file: HTTP %d", response.StatusCode)
	}

	contents, err := io.ReadAll(io.LimitReader(response.Body, maxChecksumFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("github-release: read checksum file: %w", err)
	}
	if len(contents) > maxChecksumFileSize {
		return nil, errors.New("github-release: checksum file is too large")
	}
	for _, line := range strings.Split(string(contents), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || strings.TrimPrefix(fields[1], "*") != filename {
			continue
		}
		digest, err := hex.DecodeString(fields[0])
		if err != nil || len(digest) != sha256.Size {
			return nil, fmt.Errorf("github-release: invalid SHA-256 checksum for %s", filename)
		}
		return digest, nil
	}
	return nil, fmt.Errorf("github-release: checksum for %s is missing", filename)
}

func (p *gitHubReleaseProvider) fetchArtifactSize(ctx context.Context, assetURL string) (int64, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, assetURL, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	response, err := p.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("github-release: resolve release asset: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return 0, fmt.Errorf("github-release: release asset is unavailable: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > 0 {
		return response.ContentLength, nil
	}
	return 0, nil
}

func validSemver(version string) (string, error) {
	normalized := strings.TrimSpace(version)
	if !strings.HasPrefix(normalized, "v") {
		normalized = "v" + normalized
	}
	if !semver.IsValid(normalized) {
		return "", errors.New("version must use semantic versioning")
	}
	return normalized, nil
}

type updateProgressWriter struct {
	dst        io.Writer
	written    int64
	total      int64
	onProgress func(written, total int64)
}

func (writer *updateProgressWriter) Write(data []byte) (int, error) {
	n, err := writer.dst.Write(data)
	writer.written += int64(n)
	writer.onProgress(writer.written, writer.total)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return n, err
}
