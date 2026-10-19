package proboscis

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/moschiten/moschito/internal/aedes"
)

const (
	authURL = "https://auth.docker.io/token?service=registry.docker.io&scope=repository:%s:pull"
)

type Client struct {
	Registry string
	HTTP     *http.Client
	CacheDir string
}

func NewClient(registry string, cacheDir string) *Client {
	return &Client{
		Registry: registry,
		HTTP:     &http.Client{},
		CacheDir: cacheDir,
	}
}

func (c *Client) PullImage(image string) (*aedes.Manifest, error) {
	repo := image
	tag := "latest"
	
	if strings.Contains(image, ":") {
		parts := strings.Split(image, ":")
		repo = parts[0]
		tag = parts[1]
	}

	token, err := c.getToken(repo)
	if err != nil {
		return nil, fmt.Errorf("auth failed: %w", err)
	}

	manifest, err := c.getManifest(repo, tag, token)
	if err != nil {
		return nil, fmt.Errorf("manifest fetch failed: %w", err)
	}

	for i, layer := range manifest.Layers {
		fmt.Printf("Downloading layer %d/%d (%s)\n", i+1, len(manifest.Layers), layer.Digest[:15])
		if err := c.downloadLayer(repo, layer.Digest, token); err != nil {
			return nil, fmt.Errorf("layer download failed: %w", err)
		}
	}

	return manifest, nil
}

func (c *Client) getToken(repository string) (string, error) {
	url := fmt.Sprintf(authURL, repository)
	resp, err := c.HTTP.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var authResp struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", err
	}
	return authResp.Token, nil
}

func (c *Client) getManifest(repository, tag, token string) (*aedes.Manifest, error) {
	url := fmt.Sprintf("https://%s/v2/%s/manifests/%s", c.Registry, repository, tag)
	req, _ := http.NewRequest("GET", url, nil)

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.v2+json")
	req.Header.Set("Accept", "application/vnd.docker.distribution.manifest.list.v2+json")
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	req.Header.Set("Accept", "application/vnd.oci.image.index.v1+json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("status %s: %s", resp.Status, string(body))
	}

	bodyBytes, _ := io.ReadAll(resp.Body)

	var list aedes.ManifestList
	if err := json.Unmarshal(bodyBytes, &list); err == nil && len(list.Manifests) > 0 {
		targetOS := runtime.GOOS
		targetArch := runtime.GOARCH
		if targetOS == "android" {
			targetOS = "linux"
		}

		for _, m := range list.Manifests {
			if m.Platform.OS == targetOS && m.Platform.Architecture == targetArch {
				return c.getManifest(repository, m.Digest, token)
			}
		}
		return nil, fmt.Errorf("no matching %s/%s manifest found", targetOS, targetArch)
	}

	var manifest aedes.Manifest
	json.Unmarshal(bodyBytes, &manifest)
	return &manifest, nil
}

func (c *Client) downloadLayer(repository, digest, token string) error {
	os.MkdirAll(c.CacheDir, 0755)
	parts := strings.Split(digest, ":")
	if len(parts) != 2 {
		return fmt.Errorf("invalid digest")
	}
	
	filePath := filepath.Join(c.CacheDir, parts[1])
	if _, err := os.Stat(filePath); err == nil {
		return nil // Already cached
	}

	url := fmt.Sprintf("https://%s/v2/%s/blobs/%s", c.Registry, repository, digest)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}

	out, _ := os.Create(filePath)
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}
