// Package metadata fetches Tinkerbell Hegel metadata and exposes it as
// typed Go structs. Field names and JSON tags mirror tootles' HackInstance
// shape exactly so the wire format is one source of truth.
package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Client fetches metadata from a Hegel endpoint.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New reads MIRROR_HOST (required) and METADATA_SERVICE_PORT (default
// 50061) from env and returns a Client with a 60s HTTP timeout.
func New() (*Client, error) {
	host := os.Getenv("MIRROR_HOST")
	if host == "" {
		return nil, fmt.Errorf("metadata: MIRROR_HOST env var is required")
	}
	port := os.Getenv("METADATA_SERVICE_PORT")
	if port == "" {
		port = "50061"
	}
	return &Client{
		BaseURL: fmt.Sprintf("http://%s:%s", host, port),
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}, nil
}

// Fetch retrieves /metadata and unmarshals into Metadata.
func (c *Client) Fetch(ctx context.Context) (*Metadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/metadata", nil)
	if err != nil {
		return nil, fmt.Errorf("metadata: build request: %w", err)
	}
	req.Header.Set("User-Agent", "tinkerbell-action")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metadata: GET %s: %w", req.URL, err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metadata: GET %s returned %s", req.URL, res.Status)
	}
	var w struct {
		Metadata Metadata `json:"metadata"`
	}
	if err := json.NewDecoder(res.Body).Decode(&w); err != nil {
		return nil, fmt.Errorf("metadata: decode body: %w", err)
	}
	return &w.Metadata, nil
}

type Metadata struct {
	Instance Instance `json:"instance"`
	Facility Facility `json:"facility,omitempty"`
}

type Instance struct {
	Hostname            string          `json:"hostname,omitempty"`
	CryptedRootPassword string          `json:"crypted_root_password,omitempty"`
	SSHKeys             []string        `json:"ssh_keys,omitempty"`
	Users               []User          `json:"users,omitempty"`
	SSHD                *SSHD           `json:"sshd,omitempty"`
	Console             *Console        `json:"console,omitempty"`
	Storage             Storage         `json:"storage,omitempty"`
	OS                  OperatingSystem `json:"operating_system_version,omitempty"`
}

type User struct {
	Username          string   `json:"username"`
	CryptedPassword   string   `json:"crypted_password,omitempty"`
	SSHAuthorizedKeys []string `json:"ssh_authorized_keys,omitempty"`
	Sudo              bool     `json:"sudo,omitempty"`
	Shell             string   `json:"shell,omitempty"`
}

type SSHD struct {
	PermitRootLogin        string `json:"permit_root_login,omitempty"`
	PasswordAuthentication *bool  `json:"password_authentication,omitempty"`
}

type Console struct {
	TTY  string `json:"tty,omitempty"`
	Baud int    `json:"baud,omitempty"`
}

type Storage struct {
	Disks        []Disk        `json:"disks,omitempty"`
	RAID         []RAID        `json:"raid,omitempty"`
	VolumeGroups []VolumeGroup `json:"volume_groups,omitempty"`
	Filesystems  []Filesystem  `json:"filesystems,omitempty"`
}

type Disk struct {
	Device     string      `json:"device"`
	Partitions []Partition `json:"partitions"`
	WipeTable  bool        `json:"wipe_table"`
}

type Partition struct {
	Label  string `json:"label"`
	Number int    `json:"number"`
	Size   uint64 `json:"size"`
}

type RAID struct {
	Name    string   `json:"name"`
	Level   string   `json:"level"`
	Devices []string `json:"devices"`
	Spare   []string `json:"spare,omitempty"`
}

type VolumeGroup struct {
	Name            string          `json:"name"`
	PhysicalVolumes []string        `json:"physical_volumes"`
	LogicalVolumes  []LogicalVolume `json:"logical_volumes"`
	Tags            []string        `json:"tags,omitempty"`
}

type LogicalVolume struct {
	Name string   `json:"name"`
	Size uint64   `json:"size"`
	Tags []string `json:"tags,omitempty"`
	Opts []string `json:"opts,omitempty"`
}

type Filesystem struct {
	Mount Mount `json:"mount"`
}

type Mount struct {
	Create struct {
		Options []string `json:"options"`
	} `json:"create"`
	Device string `json:"device"`
	Format string `json:"format"`
	Point  string `json:"point"`
}

type Facility struct {
	FacilityCode string `json:"facility_code,omitempty"`
	PlanSlug     string `json:"plan_slug,omitempty"`
}

type OperatingSystem struct {
	Distro     string `json:"distro,omitempty"`
	OsCodename string `json:"os_codename,omitempty"`
	OsSlug     string `json:"os_slug,omitempty"`
	Version    string `json:"version,omitempty"`
}
