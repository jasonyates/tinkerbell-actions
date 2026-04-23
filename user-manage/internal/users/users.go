// Package users applies metadata.instance.{Users, SSHD, root fields}
// inside a chrooted filesystem: creates users, sets passwords,
// installs authorized_keys, and rewrites global sshd_config directives.
package users

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/tinkerbell/actions/pkg/metadata"
)

// RewriteSSHDConfig replaces PermitRootLogin and PasswordAuthentication
// directives in sshd_config. Behaviour:
//   - s == nil           → input returned unchanged
//   - field empty/nil    → that specific directive left alone
//   - directive present  → existing line (commented or not) replaced in place
//   - directive absent   → new line appended
func RewriteSSHDConfig(in string, s *metadata.SSHD) string {
	if s == nil {
		return in
	}
	out := in
	if s.PermitRootLogin != "" {
		out = replaceDirective(out, "PermitRootLogin", s.PermitRootLogin)
	}
	if s.PasswordAuthentication != nil {
		val := "no"
		if *s.PasswordAuthentication {
			val = "yes"
		}
		out = replaceDirective(out, "PasswordAuthentication", val)
	}
	return out
}

// replaceDirective swaps any existing `#?<directive>\s+<anything>`
// line with `<directive> <value>`. Appends if no existing line matches.
// The result always has the replaced/appended line surrounded by \n on
// both sides so callers can grep for "\n<directive> <value>\n" reliably.
func replaceDirective(s, directive, value string) string {
	// Use [ \t]* rather than \s* so the leading-whitespace class can't
	// swallow the preceding newline when the directive is at byte 0 of
	// a multiline match.
	re := regexp.MustCompile(`(?m)^#?[ \t]*` + regexp.QuoteMeta(directive) + `[ \t]+.*$`)
	if re.MatchString(s) {
		// Prepend a \n so a match at byte 0 still has a newline before it
		// in the output (tests assert "\n<directive> <value>\n" is present).
		if !strings.HasPrefix(s, "\n") {
			s = "\n" + s
		}
		return re.ReplaceAllString(s, directive+" "+value)
	}
	if !strings.HasSuffix(s, "\n") {
		s += "\n"
	}
	return s + directive + " " + value + "\n"
}

// --- Imperative helpers, called from chroot ---

// SetPassword applies a crypted password via chpasswd -e. The
// cryptedPassword must be a /etc/shadow-format hash (starts with
// $6$ for sha512crypt, etc.). Not a plaintext password.
func SetPassword(user, cryptedPassword string) error {
	cmd := exec.Command("chpasswd", "-e")
	cmd.Stdin = strings.NewReader(user + ":" + cryptedPassword + "\n")
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

// EnsureUser creates u.Username via useradd -m if it doesn't already
// exist. Shell defaults to /bin/bash.
func EnsureUser(u metadata.User) error {
	if err := exec.Command("id", "-u", u.Username).Run(); err == nil {
		return nil // user exists
	}
	shell := u.Shell
	if shell == "" {
		shell = "/bin/bash"
	}
	return run("useradd", "-m", "-s", shell, u.Username)
}

// AddToGroup adds user to the named group (e.g. "sudo").
func AddToGroup(user, group string) error {
	return run("gpasswd", "-a", user, group)
}

// WriteAuthorizedKeys writes ~/.ssh/authorized_keys for user with the
// given keys, correct ownership and 0600 permission. Home is /root
// for user "root", else /home/<user>.
func WriteAuthorizedKeys(user string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	home := "/root"
	if user != "root" {
		home = filepath.Join("/home", user)
	}
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", sshDir, err)
	}
	path := filepath.Join(sshDir, "authorized_keys")
	body := strings.Join(keys, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	// chown the whole .ssh dir so file and parent match.
	return run("chown", "-R", user+":"+user, sshDir)
}

func run(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}
