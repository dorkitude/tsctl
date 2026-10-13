// Package tsapi is a small client for the Tailscale API v2.
//
// Credentials are resolved in this order, and never printed:
//  1. TS_API_KEY: a Tailscale API access token (tskey-api-...)
//  2. TS_OAUTH_CLIENT_ID + TS_OAUTH_CLIENT_SECRET: an OAuth client (client_credentials)
//  3. An exe.dev Tailscale workload identity federation integration
//     (TSCTL_WIF_URL, default https://tailscale-wif.int.exe.xyz): its /metadata gives
//     the client ID, its /token a short-lived OIDC token, which Tailscale exchanges
//     for an API token. No secret lives on the machine.
package tsapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const DefaultBase = "https://api.tailscale.com/api/v2"
const DefaultWIFURL = "https://tailscale-wif.int.exe.xyz"

// ErrReadOnly is returned for writes when the client is read-only.
var ErrReadOnly = errors.New("refused: read-only mode (--read-only / TSCTL_READONLY) blocks writes")

// Client talks to the Tailscale API.
type Client struct {
	Base     string // API base, e.g. https://api.tailscale.com/api/v2
	Tailnet  string // "-" means the credential's own tailnet
	ReadOnly bool
	Debug    io.Writer // if set, logs METHOD path -> status (never headers or bodies)
	HTTP     *http.Client
	Getenv   func(string) string

	mu     sync.Mutex
	token  string
	source string
}

// New returns a client configured from the environment.
func New() *Client {
	c := &Client{Base: DefaultBase, Tailnet: "-", HTTP: &http.Client{Timeout: 30 * time.Second}, Getenv: os.Getenv}
	if b := os.Getenv("TSCTL_API_BASE"); b != "" {
		c.Base = strings.TrimRight(b, "/")
	}
	if t := os.Getenv("TSCTL_TAILNET"); t != "" {
		c.Tailnet = t
	}
	return c
}

// AuthSource describes where the credential came from (no secrets).
func (c *Client) AuthSource() (string, error) {
	if _, err := c.accessToken(); err != nil {
		return "", err
	}
	return c.source, nil
}

func (c *Client) accessToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" {
		return c.token, nil
	}
	env := c.Getenv
	switch {
	case env("TS_API_KEY") != "":
		c.token, c.source = env("TS_API_KEY"), "TS_API_KEY"
	case env("TS_OAUTH_CLIENT_ID") != "" && env("TS_OAUTH_CLIENT_SECRET") != "":
		tok, err := c.oauthToken(url.Values{
			"client_id":     {env("TS_OAUTH_CLIENT_ID")},
			"client_secret": {env("TS_OAUTH_CLIENT_SECRET")},
			"grant_type":    {"client_credentials"},
		}, "/oauth/token")
		if err != nil {
			return "", fmt.Errorf("oauth client: %w", err)
		}
		c.token, c.source = tok, "OAuth client "+env("TS_OAUTH_CLIENT_ID")
	default:
		wif := env("TSCTL_WIF_URL")
		if wif == "" {
			wif = DefaultWIFURL
		}
		tok, clientID, err := c.wifToken(strings.TrimRight(wif, "/"))
		if err != nil {
			return "", fmt.Errorf("no credentials: set TS_API_KEY, TS_OAUTH_CLIENT_ID/SECRET, or attach an exe.dev Tailscale WIF integration (%s): %w", wif, err)
		}
		c.token, c.source = tok, fmt.Sprintf("exe.dev WIF %s (client %s)", wif, clientID)
	}
	return c.token, nil
}

func (c *Client) wifToken(wif string) (token, clientID string, err error) {
	var meta struct {
		ClientID string `json:"client_id"`
	}
	if err := c.getJSON(wif+"/metadata", &meta); err != nil {
		return "", "", err
	}
	if meta.ClientID == "" {
		return "", "", fmt.Errorf("%s/metadata has no client_id (set it with: ssh exe.dev integrations edit <name> --metadata=client_id=...)", wif)
	}
	var idt struct {
		Token string `json:"token"`
	}
	if err := c.getJSON(wif+"/token", &idt); err != nil {
		return "", "", err
	}
	if idt.Token == "" {
		return "", "", fmt.Errorf("%s/token returned no token", wif)
	}
	tok, err := c.oauthToken(url.Values{"client_id": {meta.ClientID}, "jwt": {idt.Token}}, "/oauth/token-exchange")
	if err != nil {
		return "", "", fmt.Errorf("token exchange: %w", err)
	}
	return tok, meta.ClientID, nil
}

func (c *Client) getJSON(u string, v any) error {
	resp, err := c.HTTP.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("GET %s: HTTP %d", u, resp.StatusCode)
	}
	return json.Unmarshal(body, v)
}

func (c *Client) oauthToken(form url.Values, path string) (string, error) {
	resp, err := c.HTTP.PostForm(c.Base+path, form)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("HTTP %d: %s", resp.StatusCode, apiMessage(body))
	}
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.AccessToken == "" {
		return "", fmt.Errorf("unexpected token response")
	}
	return out.AccessToken, nil
}

// Error is a non-2xx API response.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("tailscale API: HTTP %d: %s", e.Status, e.Message) }

func apiMessage(body []byte) string {
	var m struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &m) == nil && m.Message != "" {
		return m.Message
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

// Request is one API call. Path is relative to Base and may contain "{tailnet}".
type Request struct {
	Method      string
	Path        string
	Body        []byte
	ContentType string
	Header      map[string]string
}

// Response is a raw API response.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Do sends a request. Writes are refused in read-only mode before anything is sent.
func (c *Client) Do(r Request) (*Response, error) {
	m := strings.ToUpper(r.Method)
	if c.ReadOnly && m != "GET" && m != "HEAD" && m != "OPTIONS" {
		return nil, ErrReadOnly
	}
	tok, err := c.accessToken()
	if err != nil {
		return nil, err
	}
	path := strings.ReplaceAll(r.Path, "{tailnet}", url.PathEscape(c.Tailnet))
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	req, err := http.NewRequest(m, c.Base+path, bytes.NewReader(r.Body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if r.Body != nil {
		ct := r.ContentType
		if ct == "" {
			ct = "application/json"
		}
		req.Header.Set("Content-Type", ct)
	}
	for k, v := range r.Header {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if c.Debug != nil {
		fmt.Fprintf(c.Debug, "%s %s -> %d\n", m, path, resp.StatusCode)
	}
	if resp.StatusCode/100 != 2 {
		return &Response{resp.StatusCode, resp.Header, body}, &Error{resp.StatusCode, apiMessage(body)}
	}
	return &Response{resp.StatusCode, resp.Header, body}, nil
}

// JSON sends a request with an optional JSON body and decodes a JSON response into out.
func (c *Client) JSON(method, path string, in, out any) error {
	var body []byte
	if in != nil {
		var err error
		if body, err = json.Marshal(in); err != nil {
			return err
		}
	}
	resp, err := c.Do(Request{Method: method, Path: path, Body: body})
	if err != nil {
		return err
	}
	if out != nil && len(bytes.TrimSpace(resp.Body)) > 0 {
		return json.Unmarshal(resp.Body, out)
	}
	return nil
}
