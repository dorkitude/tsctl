package tsapi

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// Device is the subset of a Tailscale device that tsctl shows. Raw keeps everything.
type Device struct {
	ID                 string   `json:"id"`
	NodeID             string   `json:"nodeId"`
	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	Addresses          []string `json:"addresses"`
	OS                 string   `json:"os"`
	User               string   `json:"user"`
	Tags               []string `json:"tags"`
	Authorized         bool     `json:"authorized"`
	KeyExpiryDisabled  bool     `json:"keyExpiryDisabled"`
	Expires            string   `json:"expires"`
	LastSeen           string   `json:"lastSeen"`
	ConnectedToControl bool     `json:"connectedToControl"`
}

// ShortName is the device's MagicDNS label (first label of Name), or Hostname.
func (d Device) ShortName() string {
	if n, _, ok := strings.Cut(d.Name, "."); ok && n != "" {
		return n
	}
	if d.Name != "" {
		return d.Name
	}
	return d.Hostname
}

// Devices lists every device in the tailnet, sorted by name.
func (c *Client) Devices() ([]Device, error) {
	var out struct {
		Devices []Device `json:"devices"`
	}
	if err := c.JSON("GET", "/tailnet/{tailnet}/devices?fields=all", nil, &out); err != nil {
		return nil, err
	}
	sort.Slice(out.Devices, func(i, j int) bool { return out.Devices[i].ShortName() < out.Devices[j].ShortName() })
	return out.Devices, nil
}

// ResolveDevice finds one device by ID, node ID, MagicDNS name, hostname or IP.
func ResolveDevice(devs []Device, ref string) (Device, error) {
	ref = strings.TrimSuffix(strings.TrimSpace(ref), ".")
	var hits []Device
	for _, d := range devs {
		switch {
		case ref == d.ID, ref == d.NodeID, strings.EqualFold(ref, strings.TrimSuffix(d.Name, ".")):
			return d, nil
		case strings.EqualFold(ref, d.ShortName()), strings.EqualFold(ref, d.Hostname):
			hits = append(hits, d)
		default:
			for _, a := range d.Addresses {
				if a == ref {
					return d, nil
				}
			}
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], nil
	case 0:
		return Device{}, fmt.Errorf("no device matches %q (try `tsctl devices list`)", ref)
	}
	names := make([]string, len(hits))
	for i, h := range hits {
		names[i] = h.ID + " (" + h.Name + ")"
	}
	return Device{}, fmt.Errorf("%q matches %d devices; use an ID: %s", ref, len(hits), strings.Join(names, ", "))
}

// FindDevice lists devices and resolves ref.
func (c *Client) FindDevice(ref string) (Device, error) {
	devs, err := c.Devices()
	if err != nil {
		return Device{}, err
	}
	return ResolveDevice(devs, ref)
}

func devPath(id, suffix string) string { return "/device/" + url.PathEscape(id) + suffix }

// SetTags replaces a device's tags.
func (c *Client) SetTags(id string, tags []string) error {
	if tags == nil {
		tags = []string{}
	}
	return c.JSON("POST", devPath(id, "/tags"), map[string]any{"tags": tags}, nil)
}

// SetKeyExpiryDisabled turns node key expiry off (true) or on (false).
func (c *Client) SetKeyExpiryDisabled(id string, disabled bool) error {
	return c.JSON("POST", devPath(id, "/key"), map[string]any{"keyExpiryDisabled": disabled}, nil)
}

// SetAuthorized authorizes or deauthorizes a device.
func (c *Client) SetAuthorized(id string, authorized bool) error {
	return c.JSON("POST", devPath(id, "/authorized"), map[string]any{"authorized": authorized}, nil)
}

// DeleteDevice removes a device from the tailnet.
func (c *Client) DeleteDevice(id string) error { return c.JSON("DELETE", devPath(id, ""), nil, nil) }

// Policy is the tailnet policy file as HuJSON text plus its ETag.
type Policy struct {
	HuJSON []byte
	ETag   string
}

// GetPolicy fetches the policy file as HuJSON (comments preserved).
func (c *Client) GetPolicy() (*Policy, error) {
	resp, err := c.Do(Request{Method: "GET", Path: "/tailnet/{tailnet}/acl", Header: map[string]string{"Accept": "application/hujson"}})
	if err != nil {
		return nil, err
	}
	return &Policy{HuJSON: resp.Body, ETag: resp.Header.Get("ETag")}, nil
}

// ValidatePolicy asks Tailscale to validate a policy (and run its tests) without saving it.
func (c *Client) ValidatePolicy(hujson []byte) error {
	resp, err := c.Do(Request{Method: "POST", Path: "/tailnet/{tailnet}/acl/validate", Body: hujson, ContentType: "application/hujson"})
	if err != nil {
		return err
	}
	// Validation failures come back as 200 with a message.
	if msg := apiMessage(resp.Body); msg != "" && msg != "{}" {
		return fmt.Errorf("policy is invalid: %s", msg)
	}
	return nil
}

// SetPolicy replaces the policy. If etag is set, it fails when the policy changed meanwhile.
func (c *Client) SetPolicy(hujson []byte, etag string) error {
	h := map[string]string{"Accept": "application/hujson"}
	if etag != "" {
		h["If-Match"] = etag
	}
	_, err := c.Do(Request{Method: "POST", Path: "/tailnet/{tailnet}/acl", Body: hujson, ContentType: "application/hujson", Header: h})
	return err
}

// Key is an auth key or API key (never includes the secret, except on create).
type Key struct {
	ID           string `json:"id"`
	Key          string `json:"key,omitempty"`
	Description  string `json:"description"`
	Created      string `json:"created"`
	Expires      string `json:"expires"`
	Revoked      string `json:"revoked,omitempty"`
	KeyType      string `json:"keyType"`
	Capabilities struct {
		Devices struct {
			Create struct {
				Reusable      bool     `json:"reusable"`
				Ephemeral     bool     `json:"ephemeral"`
				Preauthorized bool     `json:"preauthorized"`
				Tags          []string `json:"tags"`
			} `json:"create"`
		} `json:"devices"`
	} `json:"capabilities"`
}

// Keys lists the tailnet's keys.
func (c *Client) Keys() ([]Key, error) {
	var out struct {
		Keys []Key `json:"keys"`
	}
	if err := c.JSON("GET", "/tailnet/{tailnet}/keys?all=true", nil, &out); err != nil {
		return nil, err
	}
	return out.Keys, nil
}

// AuthKeyOptions configures a new auth key.
type AuthKeyOptions struct {
	Tags          []string
	Reusable      bool
	Ephemeral     bool
	Preauthorized bool
	ExpirySeconds int
	Description   string
}

// CreateAuthKey creates a device auth key. The returned Key.Key is the secret.
func (c *Client) CreateAuthKey(o AuthKeyOptions) (*Key, error) {
	create := map[string]any{"reusable": o.Reusable, "ephemeral": o.Ephemeral, "preauthorized": o.Preauthorized}
	if len(o.Tags) > 0 {
		create["tags"] = o.Tags
	}
	body := map[string]any{"capabilities": map[string]any{"devices": map[string]any{"create": create}}}
	if o.ExpirySeconds > 0 {
		body["expirySeconds"] = o.ExpirySeconds
	}
	if o.Description != "" {
		body["description"] = o.Description
	}
	var k Key
	if err := c.JSON("POST", "/tailnet/{tailnet}/keys", body, &k); err != nil {
		return nil, err
	}
	return &k, nil
}

// DeleteKey revokes a key.
func (c *Client) DeleteKey(id string) error {
	return c.JSON("DELETE", "/tailnet/{tailnet}/keys/"+url.PathEscape(id), nil, nil)
}
