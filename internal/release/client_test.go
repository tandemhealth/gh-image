package release

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"
)

type scriptedCall struct {
	wantArgs string
	stdout   string
	err      error
	wantBody string
}

type scriptedRunner struct {
	t     *testing.T
	calls []scriptedCall
	seen  int
}

func (r *scriptedRunner) Run(stdin io.Reader, args ...string) ([]byte, error) {
	r.t.Helper()
	if r.seen >= len(r.calls) {
		r.t.Fatalf("unexpected gh api call: %q", args)
	}
	call := r.calls[r.seen]
	r.seen++
	if got := strings.Join(args, " "); got != call.wantArgs {
		r.t.Fatalf("call %d args = %q, want %q", r.seen, got, call.wantArgs)
	}
	if call.wantBody != "" {
		body, err := io.ReadAll(stdin)
		if err != nil {
			r.t.Fatal(err)
		}
		if string(body) != call.wantBody {
			r.t.Fatalf("call %d body = %q, want %q", r.seen, body, call.wantBody)
		}
	}
	return []byte(call.stdout), call.err
}

func TestCheckAccessRequiresPushAndManagedPrerelease(t *testing.T) {
	run := &scriptedRunner{t: t, calls: []scriptedCall{
		{wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"main","permissions":{"push":true}}`},
		{wantArgs: "repos/acme/widgets/releases/tags/gh-image-evidence", stdout: `{"id":7,"tag_name":"gh-image-evidence","name":"Automated screenshot evidence","draft":false,"prerelease":true}`},
	}}
	c := NewClient(run)
	if _, err := c.CheckAccess("acme", "widgets"); err != nil {
		t.Fatal(err)
	}
	if run.seen != len(run.calls) {
		t.Fatalf("saw %d calls, want %d", run.seen, len(run.calls))
	}
}

func TestCheckAccessRejectsReadOnlyRepository(t *testing.T) {
	run := &scriptedRunner{t: t, calls: []scriptedCall{{
		wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"main","permissions":{"push":false}}`,
	}}}
	_, err := NewClient(run).CheckAccess("acme", "widgets")
	if err == nil || !strings.Contains(err.Error(), "push access") {
		t.Fatalf("error = %v, want push access failure", err)
	}
}

func TestInitCreatesOneNonLatestPrerelease(t *testing.T) {
	run := &scriptedRunner{t: t, calls: []scriptedCall{
		{wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"trunk","permissions":{"push":true}}`},
		{wantArgs: "repos/acme/widgets/releases/tags/gh-image-evidence", err: errors.New("HTTP 404")},
		{wantArgs: "--method POST repos/acme/widgets/releases --input -", wantBody: `{"body":"Stable storage for screenshots uploaded by the gh-image extension.","draft":false,"make_latest":"false","name":"Automated screenshot evidence","prerelease":true,"tag_name":"gh-image-evidence","target_commitish":"trunk"}` + "\n", stdout: `{"id":8,"tag_name":"gh-image-evidence","name":"Automated screenshot evidence","draft":false,"prerelease":true,"html_url":"https://github.com/acme/widgets/releases/tag/gh-image-evidence"}`},
	}}
	result, err := NewClient(run).Init("acme", "widgets")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || result.Release.HTMLURL == "" {
		t.Fatalf("result = %#v", result)
	}
}

func TestUploadReusesMatchingAssetBeyondFirstPage(t *testing.T) {
	digest := strings.Repeat("a", 64)
	assetName := digest + ".png"
	url := "https://github.com/acme/widgets/releases/download/gh-image-evidence/" + assetName
	pages := fmt.Sprintf(`[[{"name":"other.png","state":"uploaded","size":1}],[{"name":%q,"state":"uploaded","size":3,"browser_download_url":%q}]]`, assetName, url)
	run := &scriptedRunner{t: t, calls: []scriptedCall{
		{wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"main","permissions":{"push":true}}`},
		{wantArgs: "repos/acme/widgets/releases/tags/gh-image-evidence", stdout: `{"id":7,"tag_name":"gh-image-evidence","name":"Automated screenshot evidence","draft":false,"prerelease":true}`},
		{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: pages},
	}}
	result, err := NewClient(run).Upload("acme", "widgets", digest, int64(3), bytes.NewBufferString("png"))
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != url || result.Uploaded {
		t.Fatalf("result = %#v", result)
	}
}

func TestUploadWaitsForConcurrentAssetAfterDuplicate(t *testing.T) {
	digest := strings.Repeat("b", 64)
	name := digest + ".png"
	url := "https://github.com/acme/widgets/releases/download/gh-image-evidence/" + name
	run := &scriptedRunner{t: t, calls: []scriptedCall{
		{wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"main","permissions":{"push":true}}`},
		{wantArgs: "repos/acme/widgets/releases/tags/gh-image-evidence", stdout: `{"id":7,"tag_name":"gh-image-evidence","name":"Automated screenshot evidence","draft":false,"prerelease":true}`},
		{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: `[]`},
		{wantArgs: "--method POST -H Content-Type: image/png --input - https://uploads.github.com/repos/acme/widgets/releases/7/assets?name=" + name, wantBody: "png", err: errors.New("HTTP 422 already_exists")},
		{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: fmt.Sprintf(`[[{"name":%q,"state":"starter","size":0}]]`, name)},
		{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: fmt.Sprintf(`[[{"name":%q,"state":"uploaded","size":3,"browser_download_url":%q}]]`, name, url)},
	}}
	c := NewClient(run)
	c.sleep = func(time.Duration) {}
	result, err := c.Upload("acme", "widgets", digest, 3, bytes.NewBufferString("png"))
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != url || result.Uploaded {
		t.Fatalf("result = %#v", result)
	}
}

func TestUploadCreatesContentAddressedAsset(t *testing.T) {
	digest := strings.Repeat("d", 64)
	name := digest + ".png"
	url := "https://github.com/acme/widgets/releases/download/gh-image-evidence/" + name
	run := &scriptedRunner{t: t, calls: []scriptedCall{
		{wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"main","permissions":{"push":true}}`},
		{wantArgs: "repos/acme/widgets/releases/tags/gh-image-evidence", stdout: `{"id":7,"tag_name":"gh-image-evidence","name":"Automated screenshot evidence","draft":false,"prerelease":true}`},
		{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: `[]`},
		{wantArgs: "--method POST -H Content-Type: image/png --input - https://uploads.github.com/repos/acme/widgets/releases/7/assets?name=" + name, wantBody: "png", stdout: fmt.Sprintf(`{"name":%q,"state":"uploaded","size":3,"browser_download_url":%q}`, name, url)},
	}}
	result, err := NewClient(run).Upload("acme", "widgets", digest, 3, bytes.NewBufferString("png"))
	if err != nil {
		t.Fatal(err)
	}
	if result.URL != url || !result.Uploaded {
		t.Fatalf("result = %#v", result)
	}
}

func TestUploadStopsWhenPendingAssetNeverCompletes(t *testing.T) {
	digest := strings.Repeat("e", 64)
	name := digest + ".png"
	pending := fmt.Sprintf(`[[{"name":%q,"state":"starter","size":0}]]`, name)
	calls := []scriptedCall{
		{wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"main","permissions":{"push":true}}`},
		{wantArgs: "repos/acme/widgets/releases/tags/gh-image-evidence", stdout: `{"id":7,"tag_name":"gh-image-evidence","name":"Automated screenshot evidence","draft":false,"prerelease":true}`},
		{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: pending},
	}
	for range 3 {
		calls = append(calls, scriptedCall{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: pending})
	}
	run := &scriptedRunner{t: t, calls: calls}
	c := NewClient(run)
	c.attempts = 3
	c.sleep = func(time.Duration) {}
	_, err := c.Upload("acme", "widgets", digest, 3, bytes.NewBufferString("png"))
	if err == nil || !strings.Contains(err.Error(), "did not reach uploaded state") {
		t.Fatalf("error = %v", err)
	}
}

func TestUploadRefusesSameNameWithDifferentSize(t *testing.T) {
	digest := strings.Repeat("c", 64)
	name := digest + ".png"
	run := &scriptedRunner{t: t, calls: []scriptedCall{
		{wantArgs: "repos/acme/widgets", stdout: `{"default_branch":"main","permissions":{"push":true}}`},
		{wantArgs: "repos/acme/widgets/releases/tags/gh-image-evidence", stdout: `{"id":7,"tag_name":"gh-image-evidence","name":"Automated screenshot evidence","draft":false,"prerelease":true}`},
		{wantArgs: "--paginate --slurp repos/acme/widgets/releases/7/assets?per_page=100", stdout: fmt.Sprintf(`[[{"name":%q,"state":"uploaded","size":4}]]`, name)},
	}}
	_, err := NewClient(run).Upload("acme", "widgets", digest, 3, bytes.NewBufferString("png"))
	if err == nil || !strings.Contains(err.Error(), "size") {
		t.Fatalf("error = %v, want size mismatch", err)
	}
}

func TestUploadRejectsInvalidDigestBeforeGitHubCall(t *testing.T) {
	run := &scriptedRunner{t: t}
	_, err := NewClient(run).Upload("acme", "widgets", "ABC", 3, bytes.NewBufferString("png"))
	if err == nil || run.seen != 0 {
		t.Fatalf("error = %v, calls = %d", err, run.seen)
	}
}
