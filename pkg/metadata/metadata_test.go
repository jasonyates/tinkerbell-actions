package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFetch_parsesFullFixture(t *testing.T) {
	fixture, err := os.ReadFile("testdata/full.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metadata" {
			http.Error(w, "not found", 404)
			return
		}
		w.Write(fixture)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: http.DefaultClient}
	md, err := c.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if md.Instance.Hostname == "" {
		t.Errorf("hostname missing")
	}
	if md.Instance.Console == nil || md.Instance.Console.TTY != "ttyS1" {
		t.Errorf("console.tty: %+v", md.Instance.Console)
	}
	if len(md.Instance.Users) != 1 || md.Instance.Users[0].Username != "ubuntu" {
		t.Errorf("users: %+v", md.Instance.Users)
	}
	if md.Instance.SSHD == nil || md.Instance.SSHD.PermitRootLogin != "prohibit-password" {
		t.Errorf("sshd.permit_root_login: %+v", md.Instance.SSHD)
	}
	if md.Instance.SSHD.PasswordAuthentication == nil || *md.Instance.SSHD.PasswordAuthentication != false {
		t.Errorf("sshd.password_authentication: %+v", md.Instance.SSHD.PasswordAuthentication)
	}
	if len(md.Instance.Storage.VolumeGroups) == 0 {
		t.Errorf("storage.volume_groups missing")
	}
	if md.Instance.Storage.VolumeGroups[0].Name != "vg0" {
		t.Errorf("vg name: %q", md.Instance.Storage.VolumeGroups[0].Name)
	}
	if len(md.Instance.Storage.VolumeGroups[0].LogicalVolumes) != 4 {
		t.Errorf("want 4 LVs, got %d", len(md.Instance.Storage.VolumeGroups[0].LogicalVolumes))
	}
	if len(md.Instance.Storage.Filesystems) != 6 {
		t.Errorf("want 6 filesystems, got %d", len(md.Instance.Storage.Filesystems))
	}
	if md.Instance.OS.OsCodename != "noble" {
		t.Errorf("os codename: %q", md.Instance.OS.OsCodename)
	}
}

func TestFetch_non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: http.DefaultClient}
	if _, err := c.Fetch(context.Background()); err == nil {
		t.Fatal("want error on 500")
	}
}

func TestNew_missingMirrorHost(t *testing.T) {
	// t.Setenv to "" is equivalent to unset for our os.Getenv == "" check,
	// and auto-restores any prior value at test end.
	t.Setenv("MIRROR_HOST", "")
	if _, err := New(); err == nil {
		t.Fatal("want error when MIRROR_HOST unset")
	}
}

func TestNew_defaultsPort(t *testing.T) {
	t.Setenv("MIRROR_HOST", "1.2.3.4")
	t.Setenv("METADATA_SERVICE_PORT", "")
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "http://1.2.3.4:50061" {
		t.Errorf("BaseURL = %q, want http://1.2.3.4:50061", c.BaseURL)
	}
}
