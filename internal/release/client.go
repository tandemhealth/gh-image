package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	Tag   = "gh-image-evidence"
	Title = "Automated screenshot evidence"
	body  = "Stable storage for screenshots uploaded by the gh-image extension."
)

var (
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
	slugPattern   = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	httpStatus    = regexp.MustCompile(`\(HTTP ([0-9]{3})\)`)
)

type Runner interface {
	Run(stdin io.Reader, args ...string) ([]byte, error)
}

type execRunner struct{}

type APIError struct {
	Status   int
	ExitCode int
	Message  string
}

func (e *APIError) Error() string { return e.Message }

func (execRunner) Run(stdin io.Reader, args ...string) ([]byte, error) {
	cmd := exec.Command("gh", append([]string{"api"}, args...)...)
	cmd.Stdin = stdin
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		apiErr := &APIError{Message: message}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			apiErr.ExitCode = exitErr.ExitCode()
		}
		if match := httpStatus.FindStringSubmatch(message); len(match) == 2 {
			apiErr.Status, _ = strconv.Atoi(match[1])
		}
		return nil, apiErr
	}
	return out, nil
}

type Client struct {
	run      Runner
	sleep    func(time.Duration)
	attempts int
}

func NewClient(run Runner) *Client {
	if run == nil {
		run = execRunner{}
	}
	return &Client{run: run, sleep: time.Sleep, attempts: 6}
}

func statusIs(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == status
}

type Repository struct {
	DefaultBranch string `json:"default_branch"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

type Release struct {
	ID         int64  `json:"id"`
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	HTMLURL    string `json:"html_url"`
}

type Asset struct {
	Name               string `json:"name"`
	State              string `json:"state"`
	Size               int64  `json:"size"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type InitResult struct {
	Release Release
	Created bool
}

type UploadResult struct {
	URL      string
	Uploaded bool
}

func validateTarget(owner, repo string) error {
	if !slugPattern.MatchString(owner) || !slugPattern.MatchString(repo) {
		return fmt.Errorf("repository must be in owner/repo format using GitHub-safe characters")
	}
	return nil
}

func (c *Client) repository(owner, repo string) (Repository, error) {
	var result Repository
	out, err := c.run.Run(nil, fmt.Sprintf("repos/%s/%s", owner, repo))
	if err != nil {
		return result, fmt.Errorf("checking repository access: %w", err)
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return result, fmt.Errorf("decoding repository response: %w", err)
	}
	if result.DefaultBranch == "" {
		return result, fmt.Errorf("repository response did not include a default branch")
	}
	if !result.Permissions.Push {
		return result, fmt.Errorf("the existing gh login does not have push access to %s/%s", owner, repo)
	}
	return result, nil
}

func (c *Client) release(owner, repo string) (Release, error) {
	var result Release
	out, err := c.run.Run(nil, fmt.Sprintf("repos/%s/%s/releases/tags/%s", owner, repo, Tag))
	if err != nil {
		return result, err
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return result, fmt.Errorf("decoding release response: %w", err)
	}
	if err := validateRelease(result); err != nil {
		return result, err
	}
	return result, nil
}

func validateRelease(value Release) error {
	if value.ID <= 0 || value.TagName != Tag || value.Name != Title || value.Draft || !value.Prerelease {
		return fmt.Errorf("release %q must be a published prerelease named %q", Tag, Title)
	}
	return nil
}

func (c *Client) CheckAccess(owner, repo string) (Release, error) {
	if err := validateTarget(owner, repo); err != nil {
		return Release{}, err
	}
	if _, err := c.repository(owner, repo); err != nil {
		return Release{}, err
	}
	value, err := c.release(owner, repo)
	if err != nil {
		return Release{}, fmt.Errorf("checking evidence release (run 'gh image init --repo %s/%s' once if it is missing): %w", owner, repo, err)
	}
	return value, nil
}

func (c *Client) Init(owner, repo string) (InitResult, error) {
	if err := validateTarget(owner, repo); err != nil {
		return InitResult{}, err
	}
	repository, err := c.repository(owner, repo)
	if err != nil {
		return InitResult{}, err
	}
	existing, err := c.release(owner, repo)
	if err == nil {
		return InitResult{Release: existing}, nil
	}
	if !statusIs(err, 404) {
		return InitResult{}, fmt.Errorf("checking evidence release: %w", err)
	}
	payload := map[string]any{
		"tag_name":         Tag,
		"target_commitish": repository.DefaultBranch,
		"name":             Title,
		"body":             body,
		"draft":            false,
		"prerelease":       true,
		"make_latest":      "false",
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return InitResult{}, err
	}
	encoded = append(encoded, '\n')
	out, err := c.run.Run(bytes.NewReader(encoded), "--method", "POST", fmt.Sprintf("repos/%s/%s/releases", owner, repo), "--input", "-")
	if err != nil {
		if statusIs(err, 422) {
			existing, readErr := c.release(owner, repo)
			if readErr == nil {
				return InitResult{Release: existing}, nil
			}
		}
		return InitResult{}, fmt.Errorf("creating evidence release: %w", err)
	}
	var created Release
	if err := json.Unmarshal(out, &created); err != nil {
		return InitResult{}, fmt.Errorf("decoding created release: %w", err)
	}
	if err := validateRelease(created); err != nil {
		return InitResult{}, err
	}
	return InitResult{Release: created, Created: true}, nil
}

func (c *Client) assets(owner, repo string, releaseID int64) ([]Asset, error) {
	endpoint := fmt.Sprintf("repos/%s/%s/releases/%d/assets?per_page=100", owner, repo, releaseID)
	out, err := c.run.Run(nil, "--paginate", "--slurp", endpoint)
	if err != nil {
		return nil, fmt.Errorf("listing evidence assets: %w", err)
	}
	var pages [][]Asset
	if err := json.Unmarshal(out, &pages); err != nil {
		return nil, fmt.Errorf("decoding evidence assets: %w", err)
	}
	var result []Asset
	for _, page := range pages {
		result = append(result, page...)
	}
	return result, nil
}

func expectedURL(owner, repo, assetName string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", owner, repo, Tag, assetName)
}

func matchingAsset(assets []Asset, owner, repo, name string, size int64) (Asset, bool, error) {
	for _, asset := range assets {
		if asset.Name != name {
			continue
		}
		if asset.State != "uploaded" {
			return asset, false, nil
		}
		if asset.Size != size {
			return asset, false, fmt.Errorf("existing asset %s has size %d, expected %d; refusing to replace it", name, asset.Size, size)
		}
		wantURL := expectedURL(owner, repo, name)
		if asset.BrowserDownloadURL != wantURL {
			return asset, false, fmt.Errorf("GitHub returned an unexpected asset URL %q", asset.BrowserDownloadURL)
		}
		return asset, true, nil
	}
	return Asset{}, false, nil
}

func (c *Client) waitForAsset(owner, repo, name string, size int64, releaseID int64) (Asset, error) {
	var last Asset
	for attempt := 0; attempt < c.attempts; attempt++ {
		assets, err := c.assets(owner, repo, releaseID)
		if err != nil {
			return Asset{}, err
		}
		asset, complete, err := matchingAsset(assets, owner, repo, name, size)
		if err != nil {
			return Asset{}, err
		}
		if complete {
			return asset, nil
		}
		if asset.Name == name {
			last = asset
		}
		if attempt+1 < c.attempts {
			delay := time.Second << attempt
			if delay > 8*time.Second {
				delay = 8 * time.Second
			}
			c.sleep(delay)
		}
	}
	if last.Name == name {
		return Asset{}, fmt.Errorf("existing asset %s remained in state %q with size %d; refusing to delete or replace it", name, last.State, last.Size)
	}
	return Asset{}, fmt.Errorf("asset %s did not reach uploaded state with size %d", name, size)
}

func (c *Client) Upload(owner, repo, digest string, size int64, contents io.Reader) (UploadResult, error) {
	if !digestPattern.MatchString(digest) {
		return UploadResult{}, fmt.Errorf("evidence digest must be 64 lowercase hexadecimal characters")
	}
	if size <= 0 || contents == nil {
		return UploadResult{}, fmt.Errorf("validated PNG contents are required")
	}
	release, err := c.CheckAccess(owner, repo)
	if err != nil {
		return UploadResult{}, err
	}
	name := digest + ".png"
	assets, err := c.assets(owner, repo, release.ID)
	if err != nil {
		return UploadResult{}, err
	}
	asset, complete, err := matchingAsset(assets, owner, repo, name, size)
	if err != nil {
		return UploadResult{}, err
	}
	if complete {
		return UploadResult{URL: asset.BrowserDownloadURL}, nil
	}
	if asset.Name == name {
		asset, err = c.waitForAsset(owner, repo, name, size, release.ID)
		if err != nil {
			return UploadResult{}, err
		}
		return UploadResult{URL: asset.BrowserDownloadURL}, nil
	}
	uploadURL := fmt.Sprintf("https://uploads.github.com/repos/%s/%s/releases/%d/assets?name=%s", owner, repo, release.ID, name)
	out, uploadErr := c.run.Run(contents, "--method", "POST", "-H", "Content-Type: image/png", "--input", "-", uploadURL)
	if uploadErr == nil {
		if err := json.Unmarshal(out, &asset); err != nil {
			return UploadResult{}, fmt.Errorf("decoding uploaded asset: %w", err)
		}
		if asset.Name == name && asset.State == "uploaded" {
			verified, complete, err := matchingAsset([]Asset{asset}, owner, repo, name, size)
			if err != nil {
				return UploadResult{}, err
			}
			if complete {
				return UploadResult{URL: verified.BrowserDownloadURL, Uploaded: true}, nil
			}
		}
	}
	if uploadErr != nil && !statusIs(uploadErr, 422) {
		return UploadResult{}, fmt.Errorf("uploading evidence asset: %w", uploadErr)
	}
	asset, err = c.waitForAsset(owner, repo, name, size, release.ID)
	if err != nil {
		if uploadErr != nil {
			return UploadResult{}, fmt.Errorf("uploading evidence asset: %v; %w", uploadErr, err)
		}
		return UploadResult{}, err
	}
	return UploadResult{URL: asset.BrowserDownloadURL}, nil
}
