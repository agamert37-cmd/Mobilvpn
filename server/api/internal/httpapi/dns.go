package httpapi

import (
	"fmt"
	"os"
	"os/exec"

	"vpnapi/internal/config"
)

// applyDNSBlocklistPolicy toggles the shared unbound resolver's ad/tracker
// blocklist by writing (or clearing) a small include file and asking
// unbound to reload it. This is deliberately best-effort: threatProtection
// is a bonus layered on top of the tunnel, never a precondition for the
// tunnel itself, so a box without unbound installed (e.g. this code's own
// dev/test sandbox) must not fail POST /api/v1/settings over it.
func applyDNSBlocklistPolicy(cfg config.Config, enabled bool) error {
	path := cfg.DNS.BlocklistControlFile
	if path == "" {
		return nil
	}

	content := "# threat protection disabled via POST /api/v1/settings\n"
	if enabled {
		content = "include: \"/etc/unbound/unbound.conf.d/blocklist.conf\"\n"
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("httpapi: writing %s: %w", path, err)
	}

	if _, err := exec.LookPath("unbound-control"); err != nil {
		return nil // unbound not installed on this host - nothing more to do
	}
	if out, err := exec.Command("unbound-control", "reload").CombinedOutput(); err != nil {
		return fmt.Errorf("httpapi: unbound-control reload: %w (%s)", err, string(out))
	}
	return nil
}
