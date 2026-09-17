package jenkins

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigureBuildPostsForm(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/3/configSubmit") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("content type = %q", ct)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal([]byte(r.PostForm.Get("json")), &got); err != nil {
			t.Errorf("json form value: %v", err)
		}
	}))
	defer srv.Close()

	if err := NewClient(srv.URL, "u", "t", false).ConfigureBuild(t.Context(), "api/main", 3, "v1.2", "notes"); err != nil {
		t.Fatalf("ConfigureBuild: %v", err)
	}
	if got["displayName"] != "v1.2" || got["description"] != "notes" {
		t.Errorf("submitted %v", got)
	}
}

func TestSetBuildDescriptionPostsForm(t *testing.T) {
	var desc string
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/7/submitDescription") {
			t.Errorf("path = %s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		desc = string(body)
	}))
	defer srv.Close()

	if err := NewClient(srv.URL, "u", "t", false).SetBuildDescription(t.Context(), "api", 7, "a b&c"); err != nil {
		t.Fatalf("SetBuildDescription: %v", err)
	}
	if desc != "description=a+b%26c" {
		t.Errorf("body = %q", desc)
	}
}

func TestParseBuildSCMDedupes(t *testing.T) {
	data := `{"actions":[{},
		{"lastBuiltRevision":{"SHA1":"abc","branch":[{"name":"origin/main"}]},"remoteUrls":["https://g/r.git"]},
		{"lastBuiltRevision":{"SHA1":"abc","branch":[{"name":"origin/main"}]},"remoteUrls":["https://g/r.git"]},
		{"lastBuiltRevision":{"SHA1":"def","branch":[]},"remoteUrls":["https://g/lib.git"]}]}`
	revs, err := parseBuildSCM([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 || revs[0].Revision != "abc" || revs[0].Branches[0] != "origin/main" || revs[1].RemoteURLs[0] != "https://g/lib.git" {
		t.Errorf("revs = %+v", revs)
	}
}

func TestGetControllerStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Jenkins", "2.500")
		_, _ = io.WriteString(w, `{"mode":"NORMAL","quietingDown":true}`)
	}))
	defer srv.Close()

	st, err := NewClient(srv.URL, "u", "t", false).GetControllerStatus(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if st.Version != "2.500" || st.Mode != "NORMAL" || !st.QuietingDown {
		t.Errorf("status = %+v", st)
	}
}
