//go:build integration

package integration_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/webtest"

	"github.com/JaimeStill/capstone-mikoshi/integration"
)

var (
	shellTitle  = regexp.MustCompile(`<title>([^<]*)</title>`)
	shellEntry  = regexp.MustCompile(`<script type="module" src="([^"]+)"></script>`)
	shellConfig = regexp.MustCompile(`<script type="application/json" id="` + web.ConfigElementID + `">([^<]*)</script>`)
)

type clientConfig struct {
	APIBase string `json:"apiBase"`
}

type shell struct {
	body   string
	entry  string
	config clientConfig
}

func getShell(t *testing.T, c *webtest.Client, path string) shell {
	t.Helper()
	res := c.Get(t, path).Expect(t, http.StatusOK)
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("GET %s Content-Type = %q, want text/html", path, ct)
	}
	body := string(res.Body)
	title := shellTitle.FindStringSubmatch(body)
	if title == nil || title[1] != "Mikoshi" {
		t.Fatalf("GET %s title = %v, want Mikoshi; body:\n%s", path, title, body)
	}
	entry := shellEntry.FindStringSubmatch(body)
	if entry == nil {
		t.Fatalf("GET %s has no entry module script; body:\n%s", path, body)
	}
	raw := shellConfig.FindStringSubmatch(body)
	if raw == nil {
		t.Fatalf("GET %s has no #%s element; body:\n%s", path, web.ConfigElementID, body)
	}
	var cfg clientConfig
	if err := json.Unmarshal([]byte(raw[1]), &cfg); err != nil {
		t.Fatalf("GET %s config %q: %v", path, raw[1], err)
	}
	return shell{body: body, entry: entry[1], config: cfg}
}

func TestClient_ServesShellAtRoot(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	sh := getShell(t, s.Client(), "/")

	if sh.config.APIBase != "/api/" {
		t.Errorf("config apiBase = %q, want /api/", sh.config.APIBase)
	}
	if !strings.HasPrefix(sh.entry, "assets/main-") || !strings.HasSuffix(sh.entry, ".js") {
		t.Errorf("entry module = %q, want the build's hashed main module", sh.entry)
	}
}

func TestClient_DeepLinkGetsShell(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	root := getShell(t, c, "/")
	deep := getShell(t, c, "/contexts/a/b")
	if deep.body != root.body {
		t.Errorf("deep link shell differs from /:\n%s\nwant:\n%s", deep.body, root.body)
	}
}

func TestClient_ServesReferencedAssetImmutable(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()
	sh := getShell(t, c, "/")

	res := c.Get(t, "/"+sh.entry).Expect(t, http.StatusOK)
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("Content-Type = %q, want text/javascript", ct)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Errorf("Cache-Control = %q, want the immutable policy for a hashed asset", cc)
	}
	if len(res.Body) == 0 || strings.Contains(string(res.Body), "<!doctype html>") {
		t.Errorf("body is not the module:\n%s", res.Body)
	}
}

func TestClient_MissingAssetIsProblem(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	p := s.Client().Get(t, "/assets/missing-0000.js").Problem(t, http.StatusNotFound)
	if p.Instance != "/assets/missing-0000.js" {
		t.Errorf("problem instance = %q, want the requested path", p.Instance)
	}
}

func TestClient_ProbesAndAPIAreNotTheShell(t *testing.T) {
	s := integration.Start(t, integration.Options{})
	c := s.Client()

	for _, path := range []string{web.HealthPath, web.ReadyPath} {
		res := c.Get(t, path).Expect(t, http.StatusOK)
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("GET %s Content-Type = %q, want application/json", path, ct)
		}
		var body map[string]any
		res.JSON(t, &body)
		if body["status"] == nil {
			t.Errorf("GET %s body = %s, want a status", path, res.Body)
		}
	}

	res := c.Get(t, "/api/contexts")
	if strings.HasPrefix(res.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("GET /api/contexts answered the shell:\n%s", res.Body)
	}
	if p := res.Problem(t, http.StatusNotFound); p.Instance != "/api/contexts" {
		t.Errorf("problem instance = %q, want /api/contexts", p.Instance)
	}
}
