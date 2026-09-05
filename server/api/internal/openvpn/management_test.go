package openvpn

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeManagementServer mimics just enough of the OpenVPN management
// interface protocol (optional password gate, banner, "status 3",
// "kill <cn>") to exercise our client parsing without needing a real
// openvpn process. Leave password empty to simulate an unauthenticated
// interface (still a valid mode this package supports, just not what the
// install scripts default to — see manager.go's ManagementPassword).
func fakeManagementServer(t *testing.T, password, statusBody string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeConn(conn, password, statusBody)
		}
	}()
	return ln.Addr().String()
}

func handleFakeConn(conn net.Conn, password, statusBody string) {
	defer conn.Close()
	reader := bufio.NewReader(conn)

	if password != "" {
		if _, err := conn.Write([]byte("ENTER PASSWORD:")); err != nil {
			return
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		if strings.TrimSpace(line) != password {
			_, _ = conn.Write([]byte("ENTER PASSWORD:")) // real openvpn re-prompts; test clients treat this as failure
			return
		}
		if _, err := conn.Write([]byte("SUCCESS: password is correct\n")); err != nil {
			return
		}
	}

	_, _ = conn.Write([]byte(">INFO:OpenVPN Management Interface Version 5 -- type 'help' for more info\n"))
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(line)
		switch {
		case cmd == "status 3":
			_, _ = conn.Write([]byte(statusBody))
		case strings.HasPrefix(cmd, "kill "):
			name := strings.TrimPrefix(cmd, "kill ")
			if name == "known-client" {
				_, _ = conn.Write([]byte("SUCCESS: common name 'known-client' found, 1 client(s) killed\n"))
			} else {
				_, _ = conn.Write([]byte("ERROR: common name '" + name + "' not found\n"))
			}
		default:
			_, _ = conn.Write([]byte("ERROR: unknown command\n"))
		}
	}
}

const sampleStatus3 = "TITLE,OpenVPN 2.6.0\n" +
	"TIME,2026-01-01 00:00:00,1767225600\n" +
	"HEADER,CLIENT_LIST,Common Name,Real Address,Virtual Address,Virtual IPv6 Address,Bytes Received,Bytes Sent,Connected Since,Connected Since (time_t),Username,Client ID,Peer ID,Data Channel Cipher\n" +
	"CLIENT_LIST,sess-abc123,203.0.113.9:54321,10.77.0.2,,10485760,2097152,2026-01-01 00:00:00,1767225600,UNDEF,1,0,AES-256-GCM\n" +
	"HEADER,ROUTING_TABLE,Virtual Address,Common Name,Real Address,Last Ref,Last Ref (time_t)\n" +
	"ROUTING_TABLE,10.77.0.2,sess-abc123,203.0.113.9:54321,2026-01-01 00:00:00,1767225600\n" +
	"GLOBAL_STATS,Max bcast/mcast queue length,0\n" +
	"END\n"

func TestQueryStatusParsesClientList(t *testing.T) {
	addr := fakeManagementServer(t, "", sampleStatus3)
	c := NewClient(addr, "")

	stats, err := c.QueryStatus()
	if err != nil {
		t.Fatalf("QueryStatus: %v", err)
	}
	if len(stats) != 1 {
		t.Fatalf("got %d clients, want 1: %+v", len(stats), stats)
	}
	got := stats[0]
	if got.CommonName != "sess-abc123" {
		t.Errorf("CommonName = %q, want sess-abc123", got.CommonName)
	}
	if got.VirtualAddress != "10.77.0.2" {
		t.Errorf("VirtualAddress = %q, want 10.77.0.2", got.VirtualAddress)
	}
	if got.BytesReceived != 10485760 || got.BytesSent != 2097152 {
		t.Errorf("bytes = (%d, %d), want (10485760, 2097152)", got.BytesReceived, got.BytesSent)
	}
	if got.ConnectedSince.IsZero() {
		t.Errorf("ConnectedSince was not parsed")
	}
}

func TestFindClientMissingReturnsNilNotError(t *testing.T) {
	addr := fakeManagementServer(t, "", sampleStatus3)
	c := NewClient(addr, "")

	stat, err := c.FindClient("no-such-session")
	if err != nil {
		t.Fatalf("FindClient: %v", err)
	}
	if stat != nil {
		t.Fatalf("expected nil for a session that isn't connected, got %+v", stat)
	}
}

func TestKillSuccessAndIdempotentMissing(t *testing.T) {
	addr := fakeManagementServer(t, "", sampleStatus3)
	c := NewClient(addr, "")

	if err := c.Kill("known-client"); err != nil {
		t.Fatalf("Kill(known-client): %v", err)
	}
	// Killing a session that isn't connected must not be an error: that is
	// already the desired end state for a disconnect call.
	if err := c.Kill("already-gone"); err != nil {
		t.Fatalf("Kill(already-gone) should be idempotent, got error: %v", err)
	}
}

func TestPasswordProtectedManagementAuthenticates(t *testing.T) {
	addr := fakeManagementServer(t, "s3cr3t-mgmt-pass", sampleStatus3)

	authed := NewClient(addr, "s3cr3t-mgmt-pass")
	if _, err := authed.QueryStatus(); err != nil {
		t.Fatalf("QueryStatus with the correct password: %v", err)
	}

	wrongPass := NewClient(addr, "wrong-password")
	if _, err := wrongPass.QueryStatus(); err == nil {
		t.Fatalf("expected an error authenticating with the wrong password")
	}

	noPass := NewClient(addr, "")
	if _, err := noPass.QueryStatus(); err == nil {
		t.Fatalf("expected an error when skipping auth against a password-protected interface")
	}
}

func TestMgmtCommandDialTimeout(t *testing.T) {
	// 203.0.113.0/24 is TEST-NET-3 (RFC 5737): guaranteed non-routable, so
	// this reliably exercises the timeout/error path without depending on
	// network conditions. A short timeout keeps the test fast.
	c := &Client{Addr: "203.0.113.1:1", Timeout: 200 * time.Millisecond}
	if _, err := c.command("status 3"); err == nil {
		t.Fatalf("expected an error dialing an unreachable management socket")
	}
}
