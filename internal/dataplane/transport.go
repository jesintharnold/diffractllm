package dataplane

import (
	"bytes"
	"context"
	"crypto/tls"
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.uber.org/zap"
	"golang.org/x/net/proxy"
)

func compareConfig[T int | int64 | time.Duration](base T, override *T) T {
	if override != nil && *override > 0 {
		return *override
	}
	return base
}

func isBlockedIP(ip netip.Addr) bool {

	ip = ip.Unmap()
	return ip.IsLoopback() ||
		ip.IsPrivate() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

func guardedControl(allowPrivate bool) func(string, string, syscall.RawConn) error {
	if allowPrivate {
		return nil
	}

	return func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return fmt.Errorf("unresolved dial address %q", address)
		}

		if isBlockedIP(ip) {
			return fmt.Errorf("blocked address %s (set allow_private_network to permit in the network provider config)", ip)
		}
		return nil
	}
}

func guardedProxyDial(d proxy.ContextDialer, allowPrivate bool) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if allowPrivate {
			return d.DialContext(ctx, network, addr)
		}

		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}

		ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, err
		}

		var lastErr error
		for _, ip := range ips {
			if isBlockedIP(ip) {
				lastErr = fmt.Errorf("upstream %s resolves to blocked address %s "+"(set allow_private_network to permit)", host, ip)
				continue
			}
			conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no addresses for %s", host)
		}
		return nil, lastErr
	}
}

func applyProxy(transport *http.Transport, dialer *net.Dialer, proxyConfig *core.ProxyConfig, allowPrivateNetwork bool) error {
	if proxyConfig == nil {
		return nil
	}

	switch proxyConfig.Type {
	case core.ProxyEnvironment:
		transport.Proxy = http.ProxyFromEnvironment
		return nil
	case core.ProxyHTTP:
		urlConfig, err := url.Parse(proxyConfig.URL)
		if err != nil {
			return fmt.Errorf("parsing proxy url: %w", err)
		}

		if proxyConfig.Username != "" {
			urlConfig.User = url.UserPassword(proxyConfig.Username, proxyConfig.Password)
		}

		transport.Proxy = http.ProxyURL(urlConfig)
	case core.ProxySOCKS5:
		var auth *proxy.Auth
		if proxyConfig.Username != "" {
			auth = &proxy.Auth{User: proxyConfig.Username, Password: proxyConfig.Password}
		}
		dialer, err := proxy.SOCKS5("tcp", proxyConfig.URL, auth, dialer)
		if err != nil {
			return fmt.Errorf("building socks5 dialer: %w", err)
		}
		ctxDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return errors.New("socks5 dialer does not support contexts")
		}
		transport.DialContext = guardedProxyDial(ctxDialer, allowPrivateNetwork)
	default:
		return fmt.Errorf("unknown proxy type %q", proxyConfig.Type)
	}
	return nil
}

func newClient(defaultConfig config.UpstreamConfig, upstreamProviderConfig *core.Upstream) (*http.Client, error) {
	providerNetwork := upstreamProviderConfig.Network
	maxConns := compareConfig(defaultConfig.MaxConnsPerHost, providerNetwork.MaxConnsPerHost)
	dialer := &net.Dialer{
		Timeout:   defaultConfig.DialTimeout,
		KeepAlive: defaultConfig.KeepAlive,
		Control:   guardedControl(providerNetwork.AllowPrivateNetwork),
	}

	transport := &http.Transport{
		MaxIdleConns:          defaultConfig.MaxIdleConns,
		MaxIdleConnsPerHost:   maxConns,
		MaxConnsPerHost:       maxConns,
		IdleConnTimeout:       defaultConfig.IdleConnTimeout,
		TLSHandshakeTimeout:   defaultConfig.TLSHandshakeTimeout,
		ResponseHeaderTimeout: defaultConfig.ResponseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		DisableCompression:    defaultConfig.DisableCompression,
		WriteBufferSize:       defaultConfig.WriteBufferSize,
		ReadBufferSize:        defaultConfig.ReadBufferSize,
		ForceAttemptHTTP2:     true,
		DialContext:           dialer.DialContext,
	}

	if providerNetwork.InsecureSkipVerify {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	if err := applyProxy(transport, dialer, upstreamProviderConfig.Proxy, upstreamProviderConfig.Network.AllowPrivateNetwork); err != nil {
		return nil, err
	}

	return &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}, nil
}

var (
	ErrStreamIdle       = errors.New("upstream stopped producing (stream_idle_timeout)")
	ErrResponseTooLarge = errors.New("upstream response exceeded max_response_bytes")
)

type streamTimeoutBody struct {
	rc       io.ReadCloser
	timeout  time.Duration
	stalled  atomic.Bool
	timer    *time.Timer
	closed   sync.Once
	closeErr error
}

func newStreamTimeoutBody(rc io.ReadCloser, timeout time.Duration) *streamTimeoutBody {
	b := &streamTimeoutBody{
		rc:      rc,
		timeout: timeout,
	}
	b.timer = time.AfterFunc(timeout, func() {
		b.stalled.Store(true)
		b.Close()
	})
	return b
}

func (b *streamTimeoutBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		b.timer.Reset(b.timeout)
	}

	if err != nil && b.stalled.Load() {
		return n, ErrStreamIdle
	}

	return n, err
}
func (b *streamTimeoutBody) Close() error {
	b.closed.Do(func() {
		b.timer.Stop()
		b.closeErr = b.rc.Close()
	})
	return b.closeErr
}

type limitedBody struct {
	rc        io.ReadCloser
	remaining int64
}

func newLimitedBody(rc io.ReadCloser, limit int64) *limitedBody {
	return &limitedBody{rc: rc, remaining: limit + 1}
}

func (b *limitedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		return 0, ErrResponseTooLarge
	}

	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.rc.Read(p)
	b.remaining -= int64(n)
	if b.remaining <= 0 {
		return n, ErrResponseTooLarge
	}
	return n, err
}
func (b *limitedBody) Close() error {
	return b.rc.Close()
}

type providerTransport struct {
	client   *http.Client
	upstream *core.Upstream
}

type providerClientMap map[core.Provider]*providerTransport

type DiffractLLMTransport struct {
	providers     atomic.Pointer[providerClientMap]
	defaultConfig config.UpstreamConfig
	logger        *zap.Logger
}

type DiffractLLMTransportRequest struct {
	Method      string
	URL         string
	Body        []byte
	Headers     map[string]string
	IsStreaming bool
}

type DiffractLLMTransportResult struct {
	Status   int
	Header   http.Header
	Body     io.ReadCloser
	TTFB     time.Duration
	Attempts int
}

func NewTransport(defaultConfig config.UpstreamConfig, upstreamProviderConfig map[core.Provider]*core.Upstream, logger *zap.Logger) *DiffractLLMTransport {
	t := DiffractLLMTransport{
		defaultConfig: defaultConfig,
		logger:        logger,
	}
	providerTransporMaps := buildProviders(defaultConfig, upstreamProviderConfig, logger)
	t.providers.Store(providerTransporMaps)
	return &t
}

func buildProviders(defaultConfig config.UpstreamConfig, upstreamProviderConfig map[core.Provider]*core.Upstream, logger *zap.Logger) *providerClientMap {
	providerClientMaps := make(providerClientMap, len(upstreamProviderConfig))
	for provider, upstream := range upstreamProviderConfig {
		client, err := newClient(defaultConfig, upstream)
		if err != nil {
			logger.Error("building client", zap.String("provider", string(provider)), zap.Error(err))
			continue
		}
		providerClientMaps[provider] = &providerTransport{client: client, upstream: upstream}
	}
	return &providerClientMaps
}

func (t *DiffractLLMTransport) ServeHTTP(rctx *core.DiffractLLMContext, req *DiffractLLMTransportRequest) (*DiffractLLMTransportResult, *core.DiffractLLMError) {
	provider := rctx.Modelkey.Provider
	pt := t.providers.Load()
	if pt == nil {
		return nil, core.NewInternalError("transport", "no providers found for the request - "+string(provider), nil)
	}

	providerTransport, ok := (*pt)[provider]
	if !ok {
		return nil, core.NewInternalError("transport", "no client for provider "+string(provider), nil)
	}

	providerClient, upstream := providerTransport.client, providerTransport.upstream
	providerFullURL := req.URL

	ctx := rctx.Context()
	if !req.IsStreaming {
		if d := compareConfig(t.defaultConfig.RequestTimeout, upstream.Network.RequestTimeout); d > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
	}

	// Add a Retry architecture to the Provider calls
	maxAttempts := 1
	if upstream.Network.MaxRetries != nil && *upstream.Network.MaxRetries > 0 {
		maxAttempts += *upstream.Network.MaxRetries
	}

	backoff := 250 * time.Millisecond
	if upstream.Network.RetryBackoff != nil && *upstream.Network.RetryBackoff > 0 {
		backoff = *upstream.Network.RetryBackoff
	}

	var lastErr *core.DiffractLLMError
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, providerFullURL, bytes.NewReader(req.Body))
		if err != nil {
			return nil, core.NewInternalError("transport", "building request", err)
		}

		httpReq.ContentLength = int64(len(req.Body))

		for k, v := range upstream.Network.Headers {
			httpReq.Header.Set(k, v)
		}

		for k, v := range req.Headers {
			httpReq.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := providerClient.Do(httpReq)
		ttfb := time.Since(start)

		if err != nil {
			if ctx.Err() != nil {
				return nil, core.NewUpstreamTimeout(string(provider), providerFullURL, err)
			}

			lastErr = core.NewUpstreamUnavailable(string(provider), providerFullURL, err)

			if attempt < maxAttempts {
				if !sleepBackoff(ctx, backoff) {
					return nil, lastErr
				}
				continue
			}
			return nil, lastErr
		}

		if retryable(resp.StatusCode) && attempt < maxAttempts {
			wait := retryAfter(resp.Header, backoff, attempt)
			// Drain a bounded amount before closing: net/http can only reuse a
			// connection whose body was read to completion, so a bare Close
			// here would burn the connection and force a fresh TLS handshake
			// on every retry. Bounded, because the point is to not read a
			// hostile error body into memory.
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			if !sleepBackoff(ctx, wait) {
				return nil, core.NewUpstreamError(string(provider), providerFullURL, resp.StatusCode, "retry aborted", nil)
			}
			continue
		}

		rctx.UpstreamStatus = resp.StatusCode
		rctx.TTFB = ttfb

		captureResponseHeaders(rctx, resp.Header)
		removeHopHeaders(resp.Header)

		body := resp.Body

		if req.IsStreaming {
			idle := compareConfig(t.defaultConfig.StreamIdleTimeout, upstream.Network.StreamIdleTimeout)
			body = newStreamTimeoutBody(body, idle)
		} else {
			maxBytes := compareConfig(t.defaultConfig.MaxResponseBytes(), &upstream.Network.MaxResponseBytes)
			body = newLimitedBody(body, maxBytes)
		}

		return &DiffractLLMTransportResult{
			Status:   resp.StatusCode,
			Header:   resp.Header,
			Body:     body,
			TTFB:     ttfb,
			Attempts: attempt,
		}, nil
	}
	return nil, lastErr
}

func retryable(status int) bool {
	switch status {
	case http.StatusRequestTimeout,
		http.StatusTooManyRequests,
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout:
		return true
	}
	return false
}

func retryAfter(h http.Header, base time.Duration, attempt int) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return base * time.Duration(1<<(attempt-1)) // exponential
}

func sleepBackoff(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

var capturedResponseHeaders = []string{
	"x-request-id",
	"openai-processing-ms",
	"x-ratelimit-remaining-requests",
	"x-ratelimit-remaining-tokens",
	"x-ratelimit-reset-requests",
	"retry-after",
}

func captureResponseHeaders(rctx *core.DiffractLLMContext, h http.Header) {
	for _, k := range capturedResponseHeaders {
		if v := h.Get(k); v != "" {
			rctx.Overwrite(core.DiffractLLMContextKey("upstream."+k), v)
		}
	}
}

func removeHopHeaders(h http.Header) {
	if conn := h.Get("Connection"); conn != "" {
		for _, name := range strings.FieldsFunc(conn, func(r rune) bool { return r == ',' || r == ' ' }) {
			h.Del(name)
		}
	}
	for _, k := range []string{
		"Connection", "Proxy-Connection", "Keep-Alive",
		"Proxy-Authenticate", "Proxy-Authorization",
		"Te", "Trailer", "Transfer-Encoding", "Upgrade",
	} {
		h.Del(k)
	}
}
