// Package pdf turns an Invoice into the PDF the Team downloads: an HTML
// statement per locale rendered through Gotenberg's Chromium route, or by a
// fake in tests.
package pdf

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Renderer converts an HTML document to a PDF.
type Renderer interface {
	Render(ctx context.Context, html string) ([]byte, error)
}

const (
	gotenbergPath    = "/forms/chromium/convert/html"
	gotenbergTimeout = 30 * time.Second
	// maxPDFBytes bounds what a misbehaving renderer can make the service hold.
	maxPDFBytes = 20 << 20
)

// Gotenberg posts the document as index.html, the one file name its HTML
// route accepts as the page.
type Gotenberg struct {
	baseURL string
	client  *http.Client
}

func NewGotenberg(baseURL string) *Gotenberg {
	return &Gotenberg{baseURL: strings.TrimRight(baseURL, "/"), client: &http.Client{Timeout: gotenbergTimeout}}
}

func (g *Gotenberg) Render(ctx context.Context, html string) ([]byte, error) {
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("files", "index.html")
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(part, html); err != nil {
		return nil, err
	}
	if err := form.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.baseURL+gotenbergPath, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", form.FormDataContentType())

	res, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()

	out, err := io.ReadAll(io.LimitReader(res.Body, maxPDFBytes))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gotenberg answered %d: %s", res.StatusCode, truncate(string(out), 200))
	}

	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}

	return s[:n]
}

// Fake answers a minimal PDF-shaped document and keeps every HTML it was
// given, so a test can read what the statement said.
type Fake struct {
	mu   sync.Mutex
	html []string
	err  error
}

func NewFake() *Fake {
	return &Fake{}
}

func (f *Fake) Render(_ context.Context, html string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	f.html = append(f.html, html)

	return []byte("%PDF-1.4\n% fake statement\n" + html), nil
}

// FailWith makes every render fail until cleared with nil.
func (f *Fake) FailWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.err = err
}

// Rendered answers the HTML documents rendered so far.
func (f *Fake) Rendered() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.html...)
}

// Reset forgets what was rendered and any scripted failure.
func (f *Fake) Reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.html, f.err = nil, nil
}
