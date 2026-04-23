package serial

import (
	"os"
	"strings"
	"testing"

	"github.com/tinkerbell/actions/pkg/metadata"
)

func TestRewriteGrubDefaults_ttyS1(t *testing.T) {
	in, err := os.ReadFile("testdata/50-cloudimg-settings.cfg.input")
	if err != nil {
		t.Fatalf("read input: %v", err)
	}
	want, err := os.ReadFile("testdata/50-cloudimg-settings.cfg.ttys1.golden")
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	got := RewriteGrubDefaults(string(in), &metadata.Console{TTY: "ttyS1", Baud: 115200})
	if got != string(want) {
		t.Errorf("output mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestRewriteGrubDefaults_nilConsole_returnsUnchanged(t *testing.T) {
	in := "GRUB_CMDLINE_LINUX_DEFAULT=\"console=ttyS0\"\n"
	if got := RewriteGrubDefaults(in, nil); got != in {
		t.Errorf("nil console should pass through; got %q", got)
	}
}

func TestRewriteGrubDefaults_emptyTTY_returnsUnchanged(t *testing.T) {
	in := "GRUB_CMDLINE_LINUX_DEFAULT=\"console=ttyS0\"\n"
	if got := RewriteGrubDefaults(in, &metadata.Console{TTY: "", Baud: 9600}); got != in {
		t.Errorf("empty TTY should pass through; got %q", got)
	}
}

func TestRewriteGrubDefaults_defaultBaudWhenZero(t *testing.T) {
	in := "GRUB_CMDLINE_LINUX_DEFAULT=\"console=ttyS0\"\nGRUB_SERIAL_COMMAND=\"serial --speed=9600 --unit=0\"\n"
	got := RewriteGrubDefaults(in, &metadata.Console{TTY: "ttyS1"}) // baud=0
	if !strings.Contains(got, "ttyS1,115200") {
		t.Errorf("default baud 115200 missing; got %q", got)
	}
	if !strings.Contains(got, "--speed=115200") {
		t.Errorf("GRUB_SERIAL_COMMAND speed not rewritten; got %q", got)
	}
}

func TestRewriteGrubDefaults_noExistingConsoleEntry(t *testing.T) {
	// If input has no `console=ttyS[0-9]+` token at all, we leave
	// the cmdline alone — we don't know where in the cmdline to
	// insert it. Serial command, however, is a directive we can
	// always rewrite.
	in := "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet splash\"\nGRUB_SERIAL_COMMAND=\"serial --speed=9600 --unit=0\"\n"
	got := RewriteGrubDefaults(in, &metadata.Console{TTY: "ttyS1", Baud: 115200})
	// cmdline unchanged
	if !strings.Contains(got, "GRUB_CMDLINE_LINUX_DEFAULT=\"quiet splash\"") {
		t.Errorf("cmdline should be untouched when no console= was present; got %q", got)
	}
	// serial directive rewritten
	if !strings.Contains(got, "--speed=115200 --unit=1") {
		t.Errorf("serial directive should be rewritten; got %q", got)
	}
}
