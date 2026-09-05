package openvpn

import (
	"bufio"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// Client talks to one OpenVPN server process's management interface.
// OpenVPN itself warns ("STRONGLY discouraged and considered insecure")
// against an unauthenticated TCP management port, so the install scripts
// always provision a random per-server password (see
// scripts/30-openvpn-setup.sh) — Password is empty only in tests/manual
// setups that intentionally skip it, in which case the auth handshake
// below is simply skipped, matching plain OpenVPN behavior.
type Client struct {
	Addr     string
	Password string
	Timeout  time.Duration
}

func NewClient(addr, password string) *Client {
	return &Client{Addr: addr, Password: password, Timeout: 3 * time.Second}
}

// command opens a short-lived connection, authenticates if a password is
// configured, issues cmd, and returns its response lines (excluding the
// banner, including the terminating "END"/"SUCCESS"/"ERROR" marker itself
// so callers can inspect it).
func (c *Client) command(cmd string) ([]string, error) {
	conn, err := net.DialTimeout("tcp", c.Addr, c.Timeout)
	if err != nil {
		return nil, fmt.Errorf("openvpn: dialing management interface %s: %w", c.Addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(c.Timeout))

	reader := bufio.NewReader(conn)

	if c.Password != "" {
		// Password-protected interfaces send a bare "ENTER PASSWORD:" with
		// no trailing newline instead of the normal ">INFO:..." banner —
		// confirmed against a real openvpn 2.6 management socket, see
		// docs/SECURITY.md for the raw protocol trace.
		prompt, err := reader.ReadString(':')
		if err != nil {
			return nil, fmt.Errorf("openvpn: reading management password prompt: %w", err)
		}
		if !strings.Contains(prompt, "ENTER PASSWORD") {
			return nil, fmt.Errorf("openvpn: unexpected management prompt %q (password configured but server didn't ask for one)", prompt)
		}
		if _, err := conn.Write([]byte(c.Password + "\n")); err != nil {
			return nil, fmt.Errorf("openvpn: sending management password: %w", err)
		}
		authLine, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("openvpn: reading management auth result: %w", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(authLine), "SUCCESS") {
			return nil, fmt.Errorf("openvpn: management authentication failed: %s", strings.TrimSpace(authLine))
		}
	}

	// The interface sends a ">INFO:..." banner immediately after connecting
	// (or after a successful password), before any command is issued.
	if _, err := reader.ReadString('\n'); err != nil {
		return nil, fmt.Errorf("openvpn: reading management banner: %w", err)
	}

	if _, err := conn.Write([]byte(cmd + "\n")); err != nil {
		return nil, fmt.Errorf("openvpn: writing management command: %w", err)
	}

	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return lines, fmt.Errorf("openvpn: reading management response: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(line, ">") {
			continue // asynchronous notification, not part of this command's reply
		}
		if line == "END" || strings.HasPrefix(line, "SUCCESS:") || strings.HasPrefix(line, "ERROR:") {
			lines = append(lines, line)
			return lines, nil
		}
		lines = append(lines, line)
	}
}

// ClientStat is one CLIENT_LIST row from `status 3`.
type ClientStat struct {
	CommonName     string
	RealAddress    string
	VirtualAddress string
	BytesReceived  uint64
	BytesSent      uint64
	ConnectedSince time.Time
}

// QueryStatus fetches and parses the full connected-client list (machine-
// readable "status 3" format, stable across OpenVPN 2.4+).
func (c *Client) QueryStatus() ([]ClientStat, error) {
	lines, err := c.command("status 3")
	if err != nil {
		return nil, err
	}
	var stats []ClientStat
	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) < 9 || fields[0] != "CLIENT_LIST" {
			continue
		}
		rxBytes, _ := strconv.ParseUint(fields[5], 10, 64)
		txBytes, _ := strconv.ParseUint(fields[6], 10, 64)
		unixSince, _ := strconv.ParseInt(fields[8], 10, 64)
		var since time.Time
		if unixSince > 0 {
			since = time.Unix(unixSince, 0)
		}
		stats = append(stats, ClientStat{
			CommonName:     fields[1],
			RealAddress:    fields[2],
			VirtualAddress: fields[3],
			BytesReceived:  rxBytes,
			BytesSent:      txBytes,
			ConnectedSince: since,
		})
	}
	return stats, nil
}

// FindClient looks up one connected client by certificate common name.
// A nil, nil return means the management interface is reachable but that
// client simply isn't connected right now (e.g. issued a cert but hasn't
// dialed in yet) — not an error.
func (c *Client) FindClient(commonName string) (*ClientStat, error) {
	stats, err := c.QueryStatus()
	if err != nil {
		return nil, err
	}
	for i := range stats {
		if stats[i].CommonName == commonName {
			return &stats[i], nil
		}
	}
	return nil, nil
}

// Kill forcibly drops a live session by common name. Not finding a
// matching session is treated as success: disconnecting something that
// already isn't connected is exactly the desired end state.
func (c *Client) Kill(commonName string) error {
	lines, err := c.command("kill " + commonName)
	if err != nil {
		return err
	}
	if len(lines) == 0 {
		return fmt.Errorf("openvpn: empty response from management interface killing %q", commonName)
	}
	last := lines[len(lines)-1]
	if strings.HasPrefix(last, "ERROR:") && !strings.Contains(last, "not found") {
		return fmt.Errorf("openvpn: kill %q failed: %s", commonName, last)
	}
	return nil
}
