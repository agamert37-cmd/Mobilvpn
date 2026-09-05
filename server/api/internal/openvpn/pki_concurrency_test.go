package openvpn

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// newRealPKI builds an actual easy-rsa CA, skipping when easy-rsa isn't
// installed. This is deliberately not a fake: the bug being guarded against
// lives in easy-rsa's own unsynchronized writes to pki/index.txt and
// pki/serial, so a stub would prove nothing.
func newRealPKI(t *testing.T) *PKI {
	t.Helper()
	makeCadir, err := exec.LookPath("make-cadir")
	if err != nil {
		t.Skip("easy-rsa (make-cadir) not installed; skipping real PKI test")
	}
	dir := filepath.Join(t.TempDir(), "easyrsa")
	if out, err := exec.Command(makeCadir, dir).CombinedOutput(); err != nil {
		t.Skipf("make-cadir failed: %v\n%s", err, out)
	}
	env := append(os.Environ(), "EASYRSA_BATCH=1", "EASYRSA_REQ_CN=ConcurrencyTestCA")
	for _, args := range [][]string{{"init-pki"}, {"build-ca", "nopass"}} {
		cmd := exec.Command(filepath.Join(dir, "easyrsa"), args...)
		cmd.Dir = dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("easyrsa %v failed: %v\n%s", args, err, out)
		}
	}
	// IssueClient bundles the tls-crypt key into the client credential, so
	// the fixture needs one even though it plays no part in this test.
	if openvpnBin, err := exec.LookPath("openvpn"); err == nil {
		cmd := exec.Command(openvpnBin, "--genkey", "secret", filepath.Join(dir, "ta.key"))
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Skipf("generating ta.key failed: %v\n%s", err, out)
		}
	} else if err := os.WriteFile(filepath.Join(dir, "ta.key"), []byte("# test placeholder\n"), 0o600); err != nil {
		t.Fatalf("writing placeholder ta.key: %v", err)
	}

	return NewPKI(dir)
}

// serialsOf reads the certificate database, returning name -> serial.
// index.txt columns are: status, expiry, revocation, serial, filename, DN.
func indexEntries(t *testing.T, dir string) (serials []string, names []string) {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "pki", "index.txt"))
	if err != nil {
		t.Fatalf("opening index.txt: %v", err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) < 6 {
			continue
		}
		serials = append(serials, cols[3])
		names = append(names, cols[5])
	}
	return serials, names
}

// Issuing certificates concurrently used to corrupt the CA database:
// measured before the fix, 8 parallel issuances produced two certificates
// sharing one serial and only 7 of 8 rows in index.txt. A duplicate serial
// makes revocation hit the wrong session (the CRL lists serials); a missing
// row makes a credential unrevokable, since easyrsa revoke looks it up
// there. Both matter because revocation is how /disconnect actually ends an
// OpenVPN session.
func TestConcurrentIssuanceKeepsCADatabaseConsistent(t *testing.T) {
	pki := newRealPKI(t)

	const n = 8
	names := make([]string, n)
	for i := range names {
		names[i] = "sess_conc_" + string(rune('a'+i))
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			_, errs[i] = pki.IssueClient(name)
		}(i, name)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("IssueClient(%s) failed: %v", names[i], err)
		}
	}

	serials, indexed := indexEntries(t, pki.dir)

	seen := map[string]int{}
	for _, s := range serials {
		seen[s]++
	}
	for s, count := range seen {
		if count > 1 {
			t.Errorf("serial %s issued to %d certificates; revoking one would revoke the others", s, count)
		}
	}

	for _, name := range names {
		found := false
		for _, dn := range indexed {
			if strings.Contains(dn, "CN="+name) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%s is missing from index.txt; easyrsa revoke could never find it, so the credential would stay valid", name)
		}
	}

	if len(serials) != n {
		t.Errorf("index.txt has %d rows, want %d", len(serials), n)
	}
}
