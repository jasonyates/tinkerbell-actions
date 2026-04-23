package users

import (
	"strings"
	"testing"

	"github.com/tinkerbell/actions/pkg/metadata"
)

func TestRewriteSSHDConfig_setsPermitRootLogin(t *testing.T) {
	in := "#PermitRootLogin prohibit-password\nPasswordAuthentication yes\n"
	out := RewriteSSHDConfig(in, &metadata.SSHD{PermitRootLogin: "no"})
	if !strings.Contains(out, "\nPermitRootLogin no\n") {
		t.Errorf("want PermitRootLogin no in output\ngot:\n%s", out)
	}
	if !strings.Contains(out, "\nPasswordAuthentication yes\n") {
		t.Errorf("PasswordAuthentication should be untouched (pointer is nil)\ngot:\n%s", out)
	}
}

func TestRewriteSSHDConfig_replacesExistingPermitRootLogin(t *testing.T) {
	in := "PermitRootLogin yes\n"
	out := RewriteSSHDConfig(in, &metadata.SSHD{PermitRootLogin: "prohibit-password"})
	if !strings.Contains(out, "PermitRootLogin prohibit-password") {
		t.Errorf("existing directive should be replaced, not duplicated\ngot:\n%s", out)
	}
	if strings.Contains(out, "PermitRootLogin yes") {
		t.Errorf("old value should be gone\ngot:\n%s", out)
	}
}

func TestRewriteSSHDConfig_setsPasswordAuth(t *testing.T) {
	no := false
	in := "PasswordAuthentication yes\n#PermitRootLogin something\n"
	out := RewriteSSHDConfig(in, &metadata.SSHD{PasswordAuthentication: &no})
	if !strings.Contains(out, "\nPasswordAuthentication no\n") {
		t.Errorf("want PasswordAuthentication no\ngot:\n%s", out)
	}
}

func TestRewriteSSHDConfig_nilLeavesUnchanged(t *testing.T) {
	in := "PermitRootLogin yes\n"
	if out := RewriteSSHDConfig(in, nil); out != in {
		t.Errorf("nil sshd should pass through; got %q", out)
	}
}

func TestRewriteSSHDConfig_appendsMissingDirective(t *testing.T) {
	// Input has no PermitRootLogin directive at all → append
	in := "Port 22\n"
	out := RewriteSSHDConfig(in, &metadata.SSHD{PermitRootLogin: "no"})
	if !strings.Contains(out, "PermitRootLogin no") {
		t.Errorf("missing directive should be appended\ngot:\n%s", out)
	}
	if !strings.Contains(out, "Port 22") {
		t.Errorf("existing lines should be preserved\ngot:\n%s", out)
	}
}

func TestRewriteSSHDConfig_bothDirectives(t *testing.T) {
	yes := true
	in := "Port 22\n"
	out := RewriteSSHDConfig(in, &metadata.SSHD{
		PermitRootLogin:        "prohibit-password",
		PasswordAuthentication: &yes,
	})
	if !strings.Contains(out, "PermitRootLogin prohibit-password") {
		t.Errorf("want PermitRootLogin directive\ngot:\n%s", out)
	}
	if !strings.Contains(out, "PasswordAuthentication yes") {
		t.Errorf("want PasswordAuthentication directive\ngot:\n%s", out)
	}
}
