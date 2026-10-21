package dataplane

import (
	"bytes"
	"context"
	"crypto/tls"
	config "diffractllm/configs"
	"diffractllm/internal/core"
	"diffractllm/internal/observability/logging"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"reflect"
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

	// Every branch dials the proxy, not the upstream, so the unguarded hop
	// dialer replaces the guarded one. An http or environment proxy resolves
	// the target itself, so those two have no dial-time target guard at all -
	// only socks5 keeps one, through guardedProxyDial.
	switch proxyConfig.Type {
	case core.ProxyEnvironment:
		transport.Proxy = http.ProxyFromEnvironment
		transport.DialContext = dialer.DialContext
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
		transport.DialContext = dialer.DialContext
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
	idle := defaultConfig.MaxIdleConnsPerHost
	if idle <= 0 || idle > maxConns {
		idle = maxConns
	}
	dialer := &net.Dialer{
		Timeout:   defaultConfig.DialTimeout,
		KeepAlive: defaultConfig.KeepAlive,
		Control:   guardedControl(providerNetwork.AllowPrivateNetwork),
	}

	transport := &http.Transport{
		MaxIdleConns:          defaultConfig.MaxIdleConns,
		MaxIdleConnsPerHost:   idle,
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

	// The proxy address is operator config, never request-derived, so reaching
	// it is not an SSRF vector and must not go through guardedControl.
	if upstreamProviderConfig.Proxy != nil {
		hop := &net.Dialer{
			Timeout:   defaultConfig.DialTimeout,
			KeepAlive: defaultConfig.KeepAlive,
		}
		if err := applyProxy(transport, hop, upstreamProviderConfig.Proxy, providerNetwork.AllowPrivateNetwork); err != nil {
			return nil, err
		}
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
	lane     *lane // place in the provider's lane; released once in Close
}

func newStreamTimeoutBody(rc io.ReadCloser, timeout time.Duration, ln *lane) *streamTimeoutBody {
	b := &streamTimeoutBody{
		rc:      rc,
		timeout: timeout,
		lane:    ln,
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
const drainTimeout = time.Second

// DrainStream reads the tail after [DONE] so the conn can be reused; best effort, at most 1 s.
func DrainStream(body io.ReadCloser) {
	if b, ok := body.(*streamTimeoutBody); ok {
		b.timeout = drainTimeout // same goroutine as the reads; the timer callback never reads it
		b.timer.Reset(drainTimeout)
	}
	io.Copy(io.Discard, io.LimitReader(body, 4<<10))
}

func (b *streamTimeoutBody) Close() error {
	b.closed.Do(func() {
		b.timer.Stop()
		b.closeErr = b.rc.Close()
		b.lane.release()
	})
	return b.closeErr
}

type limitedBody struct {
	rc        io.ReadCloser
	remaining int64
	cancel    context.CancelFunc // request timeout; released on Close
	lane      *lane              // place in the provider's lane; released on Close
	closed    atomic.Bool        // first Close wins: a second release would undercount
}

func newLimitedBody(rc io.ReadCloser, limit int64, cancel context.CancelFunc, ln *lane) *limitedBody {
	return &limitedBody{rc: rc, remaining: limit + 1, cancel: cancel, lane: ln}
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
	if !b.closed.CompareAndSwap(false, true) {
		return nil
	}
	err := b.rc.Close()
	if b.cancel != nil {
		b.cancel()
	}
	b.lane.release()
	return err
}

type providerTransport struct {
	client         *http.Client
	upstream       *core.Upstream
	TrafficControl *lane // nil when max_concurrency is 0 (off)
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

func NewTransport(defaultConfig config.UpstreamConfig, upstreamProviderConfig map[core.Provider]*core.Upstream) *DiffractLLMTransport {
	logger := logging.For("dataplane")
	t := DiffractLLMTransport{
		defaultConfig: defaultConfig,
		logger:        logger,
	}
	providerTransporMaps := buildProviders(defaultConfig, upstreamProviderConfig, logger)
	t.providers.Store(providerTransporMaps)
	return &t
}

// buildProvider builds one provider's client; prev's lane is carried over so calls already running stay counted.
func buildProvider(defaultConfig config.UpstreamConfig, provider core.Provider, upstream *core.Upstream, prev *providerTransport, logger *zap.Logger) (*providerTransport, error) {
	client, err := newClient(defaultConfig, upstream)
	if err != nil {
		return nil, err
	}
	var prevLane *lane
	if prev != nil {
		prevLane = prev.TrafficControl
	}
	return &providerTransport{
		client:         client,
		upstream:       upstream,
		TrafficControl: laneFor(laneLimit(defaultConfig, upstream.Network, provider, logger), prevLane),
	}, nil
}

func buildProviders(defaultConfig config.UpstreamConfig, upstreamProviderConfig map[core.Provider]*core.Upstream, logger *zap.Logger) *providerClientMap {
	providerClientMaps := make(providerClientMap, len(upstreamProviderConfig))
	for provider, upstream := range upstreamProviderConfig {
		pt, err := buildProvider(defaultConfig, provider, upstream, nil, logger)
		if err != nil {
			logger.Error("building client", zap.String("provider", string(provider)), zap.Error(err))
			continue
		}
		providerClientMaps[provider] = pt
	}
	return &providerClientMaps
}

func (t *DiffractLLMTransport) EffectiveNetwork(stored core.NetworkConfig) core.NetworkConfig {
	effective := stored

	requestTimeout := compareConfig(t.defaultConfig.RequestTimeout, stored.RequestTimeout)
	streamIdle := compareConfig(t.defaultConfig.StreamIdleTimeout, stored.StreamIdleTimeout)
	maxConns := compareConfig(t.defaultConfig.MaxConnsPerHost, stored.MaxConnsPerHost)
	effective.RequestTimeout = &requestTimeout
	effective.StreamIdleTimeout = &streamIdle
	effective.MaxConnsPerHost = &maxConns
	concurrency := compareConfig(t.defaultConfig.MaxConcurrency, stored.MaxConcurrency)
	effective.MaxConcurrency = &concurrency

	if effective.MaxResponseBytes == 0 {
		effective.MaxResponseBytes = t.defaultConfig.MaxResponseBytes()
	}

	retries := t.defaultConfig.MaxRetries
	if stored.MaxRetries != nil && *stored.MaxRetries > 0 {
		retries = *stored.MaxRetries
	}
	backoff := t.defaultConfig.RetryBackoff
	if stored.RetryBackoff != nil && *stored.RetryBackoff > 0 {
		backoff = *stored.RetryBackoff
	}
	effective.MaxRetries = &retries
	effective.RetryBackoff = &backoff

	ambiguous := stored.RetryAmbiguousStatus != nil && *stored.RetryAmbiguousStatus
	effective.RetryAmbiguousStatus = &ambiguous

	return effective
}

// Replace rebuilds only providers whose config changed; the rest keep their client, idle pool and lane.
func (t *DiffractLLMTransport) Replace(upstreamProviderConfig map[core.Provider]*core.Upstream) []core.Provider {
	old := t.providers.Load()
	next := make(providerClientMap, len(upstreamProviderConfig))
	var failed []core.Provider
	for provider, upstream := range upstreamProviderConfig {
		var prev *providerTransport
		if old != nil {
			prev = (*old)[provider]
		}
		if prev != nil && reflect.DeepEqual(prev.upstream, upstream) {
			next[provider] = prev
			continue
		}
		pt, err := buildProvider(t.defaultConfig, provider, upstream, prev, t.logger)
		if err != nil {
			t.logger.Error("building client", zap.String("provider", string(provider)), zap.Error(err))
			failed = append(failed, provider)
			if prev != nil {
				t.logger.Warn("keeping previous client, new config failed to build",
					zap.String("provider", string(provider)))
				next[provider] = prev
			}
			continue
		}
		next[provider] = pt
	}
	t.providers.Store(&next)
	t.closeDropped(old, next)
	return failed
}

func (t *DiffractLLMTransport) closeDropped(old *providerClientMap, next providerClientMap) {
	if old == nil {
		return
	}
	for provider, previous := range *old {
		if next[provider] == previous {
			continue
		}
		if transport, ok := previous.client.Transport.(*http.Transport); ok {
			transport.CloseIdleConnections()
		}
	}
}

// ServeHTTP returns a body the caller must Close; Close releases the request timeout and the lane place.
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
	safeURL := core.SanitizeBackendURL(providerFullURL)

	ctx := rctx.Context()
	var cancel context.CancelFunc
	if !req.IsStreaming {
		if d := compareConfig(t.defaultConfig.RequestTimeout, upstream.Network.RequestTimeout); d > 0 {
			ctx, cancel = context.WithTimeout(ctx, d)
		}
	}
	ln := providerTransport.TrafficControl
	if err := ln.acquire(); err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, core.NewProviderOverloaded(string(provider), ln.concurrency())
	}
	// On success the body owns cancel and the lane place; every other return releases both here.
	handedOff := false
	defer func() {
		if handedOff {
			return
		}
		if cancel != nil {
			cancel()
		}
		ln.release()
	}()

	maxAttempts := 1 + t.defaultConfig.MaxRetries
	if upstream.Network.MaxRetries != nil && *upstream.Network.MaxRetries > 0 {
		maxAttempts = 1 + *upstream.Network.MaxRetries
	}

	backoff := t.defaultConfig.RetryBackoff
	if upstream.Network.RetryBackoff != nil && *upstream.Network.RetryBackoff > 0 {
		backoff = *upstream.Network.RetryBackoff
	}

	allowAmbiguous := upstream.Network.RetryAmbiguousStatus != nil && *upstream.Network.RetryAmbiguousStatus

	var lastErr *core.DiffractLLMError
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		rctx.UpstreamAttempts = attempt
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, providerFullURL, bytes.NewReader(req.Body))
		if err != nil {
			return nil, core.NewInternalError("transport", "building request", err)
		}

		httpReq.ContentLength = int64(len(req.Body))

		for k, v := range upstream.Network.Headers {
			if reservedHeader(k) {
				continue
			}
			httpReq.Header.Set(k, v)
		}

		for k, v := range req.Headers {
			httpReq.Header.Set(k, v)
		}

		start := time.Now()
		resp, err := providerClient.Do(httpReq)
		ttfb := time.Since(start)

		if err != nil {
			if clientGone(rctx) {
				return nil, core.NewClientCancelled("upstream")
			}
			if ctx.Err() != nil {
				return nil, core.NewUpstreamTimeout(string(provider), safeURL, err)
			}

			lastErr = core.NewUpstreamUnavailable(string(provider), safeURL, err)

			if attempt < maxAttempts {
				if !sleepBackoff(ctx, backoff) {
					if clientGone(rctx) {
						return nil, core.NewClientCancelled("retry backoff")
					}
					return nil, lastErr
				}
				continue
			}
			return nil, lastErr
		}

		rctx.UpstreamStatus = resp.StatusCode
		if retryable(resp.StatusCode, allowAmbiguous) && attempt < maxAttempts {
			wait := retryAfter(resp.Header, backoff, attempt)
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
			resp.Body.Close()
			if !sleepBackoff(ctx, wait) {
				if clientGone(rctx) {
					return nil, core.NewClientCancelled("retry backoff")
				}
				return nil, core.NewUpstreamError(string(provider), safeURL, resp.StatusCode, "retry aborted", nil)
			}
			continue
		}

		rctx.TTFB = ttfb
		rctx.Mark(core.StageUpstream)

		captureResponseHeaders(rctx, resp.Header)
		removeHopHeaders(resp.Header)

		body := resp.Body

		if req.IsStreaming {
			idle := compareConfig(t.defaultConfig.StreamIdleTimeout, upstream.Network.StreamIdleTimeout)
			body = newStreamTimeoutBody(body, idle, ln)
		} else {
			maxBytes := compareConfig(t.defaultConfig.MaxResponseBytes(), &upstream.Network.MaxResponseBytes)
			body = newLimitedBody(body, maxBytes, cancel, ln)
		}
		handedOff = true

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

// clientGone reports a client disconnect; a request_timeout expiry is DeadlineExceeded, not Canceled.
func clientGone(rctx *core.DiffractLLMContext) bool {
	return errors.Is(rctx.Context().Err(), context.Canceled)
}

func retrySafe(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return false
}

func retryAmbiguous(status int) bool {
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func retryable(status int, allowAmbiguous bool) bool {
	return retrySafe(status) || (allowAmbiguous && retryAmbiguous(status))
}

// maxRetryWait caps one retry sleep: the call keeps its lane place while it sleeps, and streams have no request timeout.
const maxRetryWait = 30 * time.Second

func retryAfter(h http.Header, base time.Duration, attempt int) time.Duration {
	if v := h.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			if secs >= int(maxRetryWait/time.Second) {
				return maxRetryWait
			}
			return time.Duration(secs) * time.Second
		}
	}
	return min(base*time.Duration(1<<(attempt-1)), maxRetryWait)
}

func sleepBackoff(ctx context.Context, d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-ctx.Done():
		return false
	}
}

func captureResponseHeaders(rctx *core.DiffractLLMContext, h http.Header) {
	for _, uh := range core.UpstreamHeaders {
		if v := h.Get(uh.Name); v != "" {
			rctx.Overwrite(uh.Key, v)
		}
	}
}

func reservedHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "api-key", "x-api-key", "x-goog-api-key",
		"content-type", "content-length", "host",
		"connection", "proxy-connection", "keep-alive",
		"proxy-authenticate", "proxy-authorization",
		"te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
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
