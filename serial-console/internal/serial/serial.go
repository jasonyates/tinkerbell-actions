// Package serial rewrites grub defaults so the installed OS routes
// its serial console to the tty+baud in metadata.instance.console.
package serial

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/tinkerbell/actions/pkg/metadata"
)

// DefaultBaud is used when Console.Baud is 0.
const DefaultBaud = 115200

// RewriteGrubDefaults takes the contents of /etc/default/grub or
// /etc/default/grub.d/50-cloudimg-settings.cfg and returns a rewritten
// copy with console= in GRUB_CMDLINE_LINUX_DEFAULT and the unit/speed
// in GRUB_SERIAL_COMMAND updated to match c. A nil or empty-TTY
// Console returns the input unchanged.
func RewriteGrubDefaults(in string, c *metadata.Console) string {
	if c == nil || c.TTY == "" {
		return in
	}
	baud := c.Baud
	if baud == 0 {
		baud = DefaultBaud
	}
	unit := ttyUnit(c.TTY)

	// Replace any existing console=ttyS<N>[,<baud>] token.
	cmdRe := regexp.MustCompile(`console=ttyS\d+(,\d+)?`)
	in = cmdRe.ReplaceAllString(in, fmt.Sprintf("console=%s,%d", c.TTY, baud))

	// Rewrite GRUB_SERIAL_COMMAND's --speed and --unit (leave other
	// flags like --word, --parity, --stop intact).
	serialRe := regexp.MustCompile(`serial --speed=\d+ --unit=\d+`)
	in = serialRe.ReplaceAllString(in, fmt.Sprintf("serial --speed=%d --unit=%d", baud, unit))

	return in
}

// ttyUnit extracts the numeric unit from a ttyS<N> string. Returns 0
// on malformed input (defensive — grub accepts unit 0).
func ttyUnit(tty string) int {
	n := strings.TrimPrefix(tty, "ttyS")
	var u int
	fmt.Sscanf(n, "%d", &u)
	return u
}
