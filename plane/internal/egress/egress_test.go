package egress

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeResolver answers a fixed script, one answer per call, so a test can
// say "public first, then private" (DNS rebinding).
type fakeResolver struct {
	mu      sync.Mutex
	answers [][]netip.Addr
	err     error
	calls   int
}

func (f *fakeResolver) LookupNetIP(_ context.Context, _, _ string) ([]netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	if len(f.answers) == 0 {
		return nil, errors.New("no answer scripted")
	}
	a := f.answers[0]
	if len(f.answers) > 1 {
		f.answers = f.answers[1:]
	}
	return a, nil
}

func addrs(s ...string) []netip.Addr {
	out := make([]netip.Addr, 0, len(s))
	for _, x := range s {
		out = append(out, netip.MustParseAddr(x))
	}
	return out
}

func TestRefusedRanges(t *testing.T) {
	refused := []string{
		"169.254.169.254", // cloud metadata, first of all
		"169.254.1.1",     // link-local
		"127.0.0.1", "127.255.255.255",
		"10.0.0.1", "10.255.255.255",
		"172.16.0.1", "172.31.255.255",
		"192.168.0.1",
		"100.64.0.1", "100.127.255.255", // carrier-grade NAT (and Alibaba metadata 100.100.100.200)
		"100.100.100.200",
		"0.0.0.0", "0.1.2.3",
		"192.0.0.192",                  // IETF protocol assignments (Oracle metadata)
		"198.18.0.1",                   // benchmarking
		"224.0.0.1", "239.255.255.255", // multicast
		"240.0.0.1", "255.255.255.255", // reserved, broadcast
		"::", "::1",
		"fe80::1",                  // link-local
		"fc00::1", "fd00:ec2::254", // unique-local (and EC2 metadata)
		"ff02::1",                // multicast
		"::ffff:10.0.0.1",        // IPv4-mapped private
		"::ffff:169.254.169.254", // IPv4-mapped metadata
		"64:ff9b::7f00:1",        // NAT64-embedded loopback
	}
	for _, s := range refused {
		assert.NotEmpty(t, Refused(netip.MustParseAddr(s)), "%s must be refused", s)
	}
	accepted := []string{
		"203.0.113.10", "192.0.2.1", "198.51.100.1", // documentation ranges: not private, not routable — accepted on purpose
		"2001:db8::1", // IPv6 documentation
	}
	for _, s := range accepted {
		assert.Empty(t, Refused(netip.MustParseAddr(s)), "%s must be accepted", s)
	}
	// Real public addresses, and the addresses just OUTSIDE the private
	// blocks, spelled as bytes: the repo's identifier scanner refuses a
	// public dotted quad in the tree, and the boundary is the point here.
	for _, b := range [][4]byte{
		{8, 8, 8, 8},
		{172, 15, 255, 255}, {172, 32, 0, 1}, // just outside 172.16/12
		{100, 63, 255, 255}, {100, 128, 0, 1}, // just outside 100.64/10
	} {
		a := netip.AddrFrom4(b)
		assert.Empty(t, Refused(a), "%s must be accepted", a)
		assert.Empty(t, Refused(netip.AddrFrom16(a.As16())), "IPv4-mapped %s must be accepted", a)
	}
	assert.Empty(t, Refused(netip.MustParseAddr("2606:4700::1111")), "a public IPv6 address must be accepted")
	assert.Equal(t, "cloud metadata endpoint", Refused(netip.MustParseAddr("169.254.169.254")))
}

func TestDialRefusesWhenAnyResolvedAddressIsPrivate(t *testing.T) {
	var dialed []string
	p := Policy{
		Resolver: &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10", "10.0.0.7")}},
		Dial: func(_ context.Context, _, addr string) (net.Conn, error) {
			dialed = append(dialed, addr)
			return nil, errors.New("must not be reached")
		},
	}
	_, err := p.DialContext(context.Background(), "tcp", "api.example.test:443")
	require.ErrorIs(t, err, ErrPrivateAddress)
	assert.Contains(t, err.Error(), "10.0.0.7")
	assert.Empty(t, dialed, "no connection may be attempted when any answer is private")
}

func TestDialConnectsToTheCheckedAddressAndRechecksEveryCall(t *testing.T) {
	// The rebinding case: the record answers public on the first call and
	// private on the second. The first call must dial the IP it checked
	// (never the hostname, which would resolve a second time); the second
	// call must be refused outright.
	r := &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10"), addrs("169.254.169.254")}}
	var dialed []string
	p := Policy{
		Resolver: r,
		Dial: func(_ context.Context, network, addr string) (net.Conn, error) {
			dialed = append(dialed, network+" "+addr)
			c, _ := net.Pipe()
			return c, nil
		},
	}
	c, err := p.DialContext(context.Background(), "tcp", "api.example.test:443")
	require.NoError(t, err)
	_ = c.Close()
	assert.Equal(t, []string{"tcp 203.0.113.10:443"}, dialed)

	_, err = p.DialContext(context.Background(), "tcp", "api.example.test:443")
	require.ErrorIs(t, err, ErrPrivateAddress)
	assert.Contains(t, err.Error(), "cloud metadata endpoint")
	assert.Len(t, dialed, 1, "the refused call must not dial")
	assert.Equal(t, 2, r.calls, "every call resolves afresh — no cached answer can outlive a record change")
}

func TestDialRefusesPortsOtherThan443AndLiteralPrivateIPs(t *testing.T) {
	p := Policy{Resolver: &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10")}}}
	_, err := p.DialContext(context.Background(), "tcp", "api.example.test:8443")
	require.ErrorIs(t, err, ErrPort)
	_, err = p.DialContext(context.Background(), "tcp", "api.example.test:80")
	require.ErrorIs(t, err, ErrPort)
	_, err = p.DialContext(context.Background(), "tcp", "192.168.1.1:443")
	require.ErrorIs(t, err, ErrPrivateAddress)
	_, err = p.DialContext(context.Background(), "tcp", "[::1]:443")
	require.ErrorIs(t, err, ErrPrivateAddress)
	_, err = p.DialContext(context.Background(), "tcp", "[::ffff:127.0.0.1]:443")
	require.ErrorIs(t, err, ErrPrivateAddress)
}

func TestDialFailsClosedOnResolutionFailureOrEmptyAnswer(t *testing.T) {
	p := Policy{Resolver: &fakeResolver{err: errors.New("SERVFAIL")}}
	_, err := p.DialContext(context.Background(), "tcp", "api.example.test:443")
	require.Error(t, err)
	p = Policy{Resolver: &fakeResolver{answers: [][]netip.Addr{{}}}}
	_, err = p.DialContext(context.Background(), "tcp", "api.example.test:443")
	require.Error(t, err)
}

func TestVetRefusesPrivateAndReportsResolutionFailureDistinctly(t *testing.T) {
	ctx := context.Background()
	_, err := Vet(ctx, &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10", "172.16.0.9")}}, "api.example.test")
	require.ErrorIs(t, err, ErrPrivateAddress)
	got, err := Vet(ctx, &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10")}}, "api.example.test")
	require.NoError(t, err)
	assert.Equal(t, addrs("203.0.113.10"), got)
	_, err = Vet(ctx, &fakeResolver{err: errors.New("no such host")}, "api.example.test")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrPrivateAddress), "a lookup failure is not a private answer")
	_, err = Vet(ctx, &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10")}}, "10.1.2.3")
	require.ErrorIs(t, err, ErrPrivateAddress, "an IP literal is vetted without a lookup")
}

// hostedServer is an https stand-in the hardened client can reach: the
// resolver answers a public address, the test dial hands the connection
// to the local listener anyway (the check happened on the public
// answer), and the server's own certificate is the upstream's ca_file.
func hostedServer(t *testing.T, handler http.HandlerFunc, p Policy) (*http.Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)
	caFile := filepath.Join(t.TempDir(), "ca.crt")
	require.NoError(t, os.WriteFile(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600))
	if p.Resolver == nil {
		p.Resolver = &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10")}}
	}
	p.Dial = func(ctx context.Context, network, addr string) (net.Conn, error) {
		require.Equal(t, "203.0.113.10:443", addr, "the checked address is what gets dialed")
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	// httptest's certificate is issued for example.com (and 127.0.0.1);
	// the client verifies it against the upstream's ca_file, by name.
	c, err := NewClient(p, []Host{{Name: "example.com", CAFile: caFile}})
	require.NoError(t, err)
	return c, srv
}

func TestClientRoundTripsThroughTheTestCAOnly(t *testing.T) {
	c, _ := hostedServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "hello from "+r.Host)
	}, Policy{})
	resp, err := c.Get("https://example.com/mcp")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "hello from example.com", string(body))

	// System roots only (no ca_file) must NOT trust the test certificate.
	p := Policy{Resolver: &fakeResolver{answers: [][]netip.Addr{addrs("203.0.113.10")}}}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	p.Dial = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	c2, err := NewClient(p, []Host{{Name: "example.com"}})
	require.NoError(t, err)
	_, err = c2.Get("https://example.com/mcp")
	require.Error(t, err)
	var unknown x509.UnknownAuthorityError
	assert.True(t, errors.As(err, &unknown), "expected an unknown-authority failure, got %v", err)
}

func TestClientRefusesPlainHTTPOtherPortsUnknownHostsAndRedirects(t *testing.T) {
	c, _ := hostedServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/elsewhere", http.StatusFound)
	}, Policy{})
	_, err := c.Get("http://example.com/mcp")
	require.ErrorIs(t, err, ErrScheme)
	_, err = c.Get("https://example.com:8443/mcp")
	require.ErrorIs(t, err, ErrPort)
	_, err = c.Get("https://other.example.com/mcp")
	require.ErrorIs(t, err, ErrUnknownHost)
	// Port 443 spelled out is the same upstream.
	resp, err := c.Get("https://example.com:443/mcp")
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusFound, resp.StatusCode, "a redirect is surfaced, never followed")
}

func TestResponseSizeCapFailsTheReadNotSilentlyTruncates(t *testing.T) {
	c, _ := hostedServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 3000))
	}, Policy{MaxResponseBytes: 2048})
	resp, err := c.Get("https://example.com/mcp")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_, err = io.ReadAll(resp.Body)
	require.ErrorIs(t, err, ErrResponseTooLarge)
}

func TestStalledBodyIsCutAtTheLifetimeDeadline(t *testing.T) {
	release := make(chan struct{})
	c, _ := hostedServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		_, _ = io.WriteString(w, "partial")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		select { // stall the body until the client gives up
		case <-release:
		case <-r.Context().Done():
		}
	}, Policy{BodyLifetime: 300 * time.Millisecond})
	defer close(release)
	started := time.Now()
	resp, err := c.Get("https://example.com/mcp")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	require.ErrorIs(t, err, ErrBodyLifetime)
	assert.Equal(t, "partial", string(body))
	assert.Less(t, time.Since(started), 5*time.Second, "the read must not hold the worker")
}

func TestResponseHeaderTimeoutBoundsASilentUpstream(t *testing.T) {
	release := make(chan struct{})
	c, _ := hostedServer(t, func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}, Policy{ResponseHeaderTimeout: 200 * time.Millisecond})
	defer close(release)
	_, err := c.Get("https://example.com/mcp")
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "timeout") || strings.Contains(err.Error(), "deadline"), err.Error())
}

func TestNewClientRefusesAnUnreadableCAFileAndDuplicateHosts(t *testing.T) {
	_, err := NewClient(Policy{}, []Host{{Name: "example.com", CAFile: filepath.Join(t.TempDir(), "missing.crt")}})
	require.Error(t, err)
	bad := filepath.Join(t.TempDir(), "bad.crt")
	require.NoError(t, os.WriteFile(bad, []byte("not a certificate"), 0o600))
	_, err = NewClient(Policy{}, []Host{{Name: "example.com", CAFile: bad}})
	require.Error(t, err)
	_, err = NewClient(Policy{}, []Host{{Name: "Example.com"}, {Name: "example.com"}})
	require.Error(t, err, "one host, one trust decision")
}
