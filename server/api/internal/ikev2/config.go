package ikev2

import (
	"fmt"
	"strings"
)

type sessionParams struct {
	Name       string
	ServerID   string
	ServerCert string
	Username   string
	Password   string
	VirtualIP  string
	DNSServers []string
}

// renderSessionConfig builds one swanctl conf.d drop-in holding everything
// a single session needs: its own connection (which only accepts its own
// EAP identity), its own one-address pool (so the virtual IP the API
// promised is the one strongSwan actually hands out), and its own
// credential.
//
// Formatting is not cosmetic here: swanctl reads a setting's value to the
// END OF LINE, so packing several settings onto one line silently produces
// an invalid value and strongSwan discards the whole connection
// ("invalid value for: local_ts, config discarded"). Every setting
// therefore gets its own line — verified against strongSwan 5.9.13.
func renderSessionConfig(p sessionParams) string {
	var b strings.Builder

	b.WriteString("# Managed by vpn-api — one file per session, do not edit by hand.\n")
	b.WriteString("# Removing this file and running `swanctl --load-all` unloads the session.\n")
	b.WriteString("connections {\n")
	fmt.Fprintf(&b, "    %s {\n", p.Name)
	b.WriteString("        version = 2\n")
	// AEAD first (fastest on modern CPUs), with a widely-compatible
	// fallback so stock mobile IKEv2 clients can still negotiate.
	b.WriteString("        proposals = aes256gcm16-prfsha384-ecp384,aes256-sha256-modp2048\n")
	b.WriteString("        local_addrs = %any\n")
	fmt.Fprintf(&b, "        pools = pool-%s\n", p.Name)
	// Mobile clients roam between networks; MOBIKE is the whole reason to
	// offer IKEv2 alongside WireGuard.
	b.WriteString("        mobike = yes\n")
	b.WriteString("        fragmentation = yes\n")
	b.WriteString("        send_certreq = no\n")
	b.WriteString("        dpd_delay = 30s\n")
	b.WriteString("        local {\n")
	b.WriteString("            auth = pubkey\n")
	fmt.Fprintf(&b, "            certs = %s\n", p.ServerCert)
	fmt.Fprintf(&b, "            id = %s\n", p.ServerID)
	b.WriteString("        }\n")
	b.WriteString("        remote {\n")
	b.WriteString("            auth = eap-mschapv2\n")
	// Scoped to this session's identity only: another session's credential
	// cannot authenticate against this connection.
	fmt.Fprintf(&b, "            eap_id = %s\n", p.Username)
	b.WriteString("        }\n")
	b.WriteString("        children {\n")
	b.WriteString("            net {\n")
	b.WriteString("                local_ts = 0.0.0.0/0\n")
	b.WriteString("                esp_proposals = aes256gcm16-ecp384,aes256-sha256\n")
	b.WriteString("                dpd_action = clear\n")
	b.WriteString("            }\n")
	b.WriteString("        }\n")
	b.WriteString("    }\n")
	b.WriteString("}\n")

	b.WriteString("pools {\n")
	fmt.Fprintf(&b, "    pool-%s {\n", p.Name)
	fmt.Fprintf(&b, "        addrs = %s/32\n", p.VirtualIP)
	if len(p.DNSServers) > 0 {
		fmt.Fprintf(&b, "        dns = %s\n", strings.Join(p.DNSServers, ","))
	}
	b.WriteString("    }\n")
	b.WriteString("}\n")

	b.WriteString("secrets {\n")
	fmt.Fprintf(&b, "    eap-%s {\n", p.Name)
	fmt.Fprintf(&b, "        id = %s\n", p.Username)
	fmt.Fprintf(&b, "        secret = %s\n", p.Password)
	b.WriteString("    }\n")
	b.WriteString("}\n")

	return b.String()
}
