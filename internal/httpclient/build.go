package httpclient

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Options sets the client policy for an execution. Zero values mean the
// defaults: 30s deadline, TLS verification on.
type Options struct {
	Timeout         time.Duration // zero → DefaultTimeout
	InsecureTLS     bool          // skip TLS certificate verification
	FollowRedirects bool          // false: return 3xx responses without following
}

// sensitiveHeaders are never forwarded to a different origin on redirect.
var sensitiveHeaders = []string{
	"Authorization", "Proxy-Authorization", "Cookie", "Cookie2",
	"Www-Authenticate", "Proxy-Authenticate",
}

// Client returns a client with the given policy: at most maxRedirects
// redirects when following, TLS verification on and no automatic retries
// unless overridden. Credentials only follow redirects that stay on the
// original origin.
func Client(opts Options) *http.Client {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	c := &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if !opts.FollowRedirects {
				return http.ErrUseLastResponse
			}
			if len(via) >= maxRedirects {
				return fmt.Errorf("stopped after %d redirects", maxRedirects)
			}
			if !sameOrigin(via[0].URL, req.URL) {
				for _, name := range sensitiveHeaders {
					req.Header.Del(name)
				}
			}
			return nil
		},
	}
	if opts.InsecureTLS {
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig.InsecureSkipVerify = true
		c.Transport = transport
	}
	return c
}

// sameOrigin compares scheme and host:port. net/http only compares the
// domain name, which would forward credentials across ports and on an
// https→http downgrade; those count as unrelated origins here.
func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(a.Host, b.Host)
}
