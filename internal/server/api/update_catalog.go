package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/mod/semver"

	"github.com/haoxin/boxfleet/internal/model"
)

const updateManifestName = "boxfleet-update.json"

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

type updateManifest struct {
	Release   string                            `json:"release"`
	Platforms map[string]updateManifestPlatform `json:"platforms"`
}

type updateManifestPlatform struct {
	Agent   updateManifestAsset `json:"agent"`
	SingBox updateManifestAsset `json:"sing_box"`
}

type updateManifestAsset struct {
	Version string `json:"version"`
	Name    string `json:"name"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
}

type updateCatalog struct {
	options Options
	mu      sync.Mutex
	loaded  *updateManifest
}

func newUpdateCatalog(options Options) *updateCatalog {
	return &updateCatalog{options: options}
}

func adminReleaseHandler(options Options, catalog *updateCatalog) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		release := adminRelease{
			Repo: releaseRepo(options), BoxFleetVersion: releaseVersion(options),
			AgentVersion:   releaseAgentVersion(options),
			SingBoxVersion: releaseSingBoxVersion(options),
		}
		if _, err := catalog.load(r.Context()); err != nil {
			release.UpdateError = err.Error()
		} else {
			release.UpdatesEnabled = true
		}
		writeJSON(w, release)
	}
}

func (c *updateCatalog) assetsForNode(ctx context.Context, r *http.Request, goos, goarch string) (model.UpdateAsset, model.UpdateAsset, error) {
	manifest, err := c.load(ctx)
	if err != nil {
		return model.UpdateAsset{}, model.UpdateAsset{}, err
	}
	key := strings.TrimSpace(goos) + "/" + strings.TrimSpace(goarch)
	platform, ok := manifest.Platforms[key]
	if !ok {
		return model.UpdateAsset{}, model.UpdateAsset{}, fmt.Errorf("release %s has no assets for %s", manifest.Release, key)
	}
	agentURL, err := c.assetURL(r, manifest.Release, platform.Agent.Name)
	if err != nil {
		return model.UpdateAsset{}, model.UpdateAsset{}, err
	}
	singBoxURL, err := c.assetURL(r, manifest.Release, platform.SingBox.Name)
	if err != nil {
		return model.UpdateAsset{}, model.UpdateAsset{}, err
	}
	return model.UpdateAsset{
			Component: "agent", Version: platform.Agent.Version, URL: agentURL,
			SHA256: platform.Agent.SHA256, Size: platform.Agent.Size,
		}, model.UpdateAsset{
			Component: "sing_box", Version: platform.SingBox.Version, URL: singBoxURL,
			SHA256: platform.SingBox.SHA256, Size: platform.SingBox.Size,
		}, nil
}

func (c *updateCatalog) load(ctx context.Context) (*updateManifest, error) {
	c.mu.Lock()
	if c.loaded != nil {
		manifest := c.loaded
		c.mu.Unlock()
		return manifest, nil
	}
	c.mu.Unlock()
	raw, err := c.readManifest(ctx)
	if err != nil {
		return nil, err
	}
	var manifest updateManifest
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode update manifest: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return nil, fmt.Errorf("decode update manifest trailing data: %w", err)
	}
	if err := c.validateManifest(manifest); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.loaded == nil {
		c.loaded = &manifest
	}
	manifestPtr := c.loaded
	c.mu.Unlock()
	return manifestPtr, nil
}

func (c *updateCatalog) readManifest(ctx context.Context) ([]byte, error) {
	if c.options.ArtifactDir != "" {
		path := filepath.Join(c.options.ArtifactDir, updateManifestName)
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open local update manifest: %w", err)
		}
		defer file.Close()
		return readLimited(file, 1024*1024)
	}
	release := strings.TrimSpace(c.options.Version)
	repo := releaseRepo(c.options)
	if !semver.IsValid(release) {
		return nil, fmt.Errorf("server version %q is not a formal release", release)
	}
	if !repositoryPattern.MatchString(repo) {
		return nil, errors.New("invalid release repository")
	}
	manifestURL := "https://github.com/" + repo + "/releases/download/" + url.PathEscape(release) + "/" + updateManifestName
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 15 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("fetch update manifest: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch update manifest: %s", response.Status)
	}
	return readLimited(response.Body, 1024*1024)
}

func (c *updateCatalog) validateManifest(manifest updateManifest) error {
	release := strings.TrimSpace(c.options.Version)
	if manifest.Release != release || !semver.IsValid(manifest.Release) {
		return fmt.Errorf("update manifest release %q does not match server release %q", manifest.Release, release)
	}
	if len(manifest.Platforms) == 0 {
		return errors.New("update manifest has no platforms")
	}
	for platform, assets := range manifest.Platforms {
		if strings.Count(platform, "/") != 1 {
			return fmt.Errorf("invalid update platform %q", platform)
		}
		if err := validateManifestAsset(assets.Agent, releaseAgentVersion(c.options), "agent"); err != nil {
			return fmt.Errorf("platform %s: %w", platform, err)
		}
		if err := validateManifestAsset(assets.SingBox, releaseSingBoxVersion(c.options), "sing_box"); err != nil {
			return fmt.Errorf("platform %s: %w", platform, err)
		}
	}
	return nil
}

func validateManifestAsset(asset updateManifestAsset, expectedVersion, component string) error {
	if asset.Version != expectedVersion || !semver.IsValid(asset.Version) {
		return fmt.Errorf("%s version %q does not match %q", component, asset.Version, expectedVersion)
	}
	if filepath.Base(asset.Name) != asset.Name || asset.Name == "." || asset.Name == "" {
		return fmt.Errorf("invalid %s asset name", component)
	}
	checksum, err := hex.DecodeString(asset.SHA256)
	if err != nil || len(checksum) != sha256.Size {
		return fmt.Errorf("invalid %s asset SHA256", component)
	}
	if asset.Size <= 0 {
		return fmt.Errorf("invalid %s asset size", component)
	}
	return nil
}

func (c *updateCatalog) assetURL(r *http.Request, release, name string) (string, error) {
	if c.options.ArtifactDir == "" {
		return "https://github.com/" + releaseRepo(c.options) + "/releases/download/" + url.PathEscape(release) + "/" + url.PathEscape(name), nil
	}
	relative := name
	if _, err := os.Stat(filepath.Join(c.options.ArtifactDir, relative)); errors.Is(err, os.ErrNotExist) {
		relative = filepath.Join("artifacts", name)
	}
	fullPath := filepath.Join(c.options.ArtifactDir, relative)
	if info, err := os.Stat(fullPath); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = errors.New("not a regular file")
		}
		return "", fmt.Errorf("local update asset %s: %w", name, err)
	}
	return requestBaseURL(r) + "/artifacts/" + strings.ReplaceAll(filepath.ToSlash(relative), " ", "%20"), nil
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	limited := io.LimitReader(reader, limit+1)
	raw, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("update manifest is too large")
	}
	return raw, nil
}
