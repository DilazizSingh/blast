// Package stripe reads a Stripe account's subscriptions and turns them into
// blast's own model. It sends GET requests and nothing else.
package stripe

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// APIURL is Stripe's API, the only place the key is ever sent.
	APIURL = "https://api.stripe.com"
	// apiVersion pins the response shapes this package was written against,
	// whatever version the account defaults to.
	apiVersion = "2026-10-28.endive"

	maxBody  = 32 << 20 // a page of 100 objects is a small fraction of this
	maxPages = 100_000  // ten million objects; a cursor that never ends stops here
	attempts = 3
)

// Client reads from the Stripe API.
type Client struct {
	key, base, agent string
	http             *http.Client
	sleep            func(time.Duration) // between retries; replaced in tests
}

// NewClient returns a client for the account the key belongs to. live states
// that the caller means to read real customer data rather than test data.
func NewClient(key, baseURL, userAgent string, live bool) (*Client, error) {
	if err := checkKey(key, live); err != nil {
		return nil, err
	}
	return &Client{
		key: key, base: baseURL, agent: userAgent, sleep: time.Sleep,
		http: &http.Client{
			Timeout: 30 * time.Second,
			// The key is for Stripe alone: a redirect is reported, not followed.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

// Live reports whether the client reads real, not test, data.
func (c *Client) Live() bool { return strings.HasPrefix(c.key, "rk_live_") }

// checkKey decides whether a key may be used at all. No message it returns
// contains the key.
func checkKey(key string, live bool) error {
	switch {
	case key == "":
		return errors.New("STRIPE_API_KEY is not set; give it a restricted, read-only key (see docs/stripe.md)")
	case strings.HasPrefix(key, "sk_live_"):
		return errors.New("STRIPE_API_KEY is a live secret key, which can move money; " +
			"create a restricted read-only key (rk_live_...) instead (see docs/stripe.md)")
	case strings.HasPrefix(key, "rk_live_") && !live:
		return errors.New("STRIPE_API_KEY is a live-mode key; add --live once the import has worked with a test-mode key")
	case strings.HasPrefix(key, "rk_live_"), strings.HasPrefix(key, "rk_test_"), strings.HasPrefix(key, "sk_test_"):
		return nil
	}
	return errors.New("STRIPE_API_KEY does not look like a Stripe key (expected rk_test_..., sk_test_... or rk_live_...)")
}

// get fetches one page and decodes it into out. A busy or failing Stripe is
// tried again; a refusal is not.
func (c *Client) get(path string, query url.Values, out any) error {
	var err error
	for attempt := range attempts {
		if attempt > 0 {
			c.sleep(time.Duration(attempt) * time.Second)
		}
		var retry bool
		if retry, err = c.try(path, query, out); !retry {
			return err
		}
	}
	return err
}

func (c *Client) try(path string, query url.Values, out any) (retry bool, err error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path+"?"+query.Encode(), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Stripe-Version", apiVersion)
	req.Header.Set("User-Agent", c.agent)

	resp, err := c.http.Do(req)
	if err != nil {
		return true, fmt.Errorf("stripe: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	switch {
	case err != nil:
		return true, fmt.Errorf("stripe: reading %s: %w", path, err)
	case len(body) > maxBody:
		return false, fmt.Errorf("stripe: %s answered with more than %d MB; refusing to read it", path, maxBody>>20)
	case resp.StatusCode != http.StatusOK:
		return resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500, apiError(path, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return false, fmt.Errorf("stripe: %s: unexpected answer: %w", path, err)
	}
	return false, nil
}

// apiError reports a refusal in Stripe's own words when it gave any.
func apiError(path string, status int, body []byte) error {
	var answer struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &answer) != nil || answer.Error.Message == "" {
		return fmt.Errorf("stripe: %s: HTTP %d", path, status)
	}
	return fmt.Errorf("stripe: %s: %s (HTTP %d)", path, answer.Error.Message, status)
}

// page is how Stripe returns a list: some objects, and whether more follow.
type page[T any] struct {
	Data    []T  `json:"data"`
	HasMore bool `json:"has_more"`
}

// list fetches every page of a collection. id names the cursor field.
func list[T any](c *Client, path string, query url.Values, id func(T) string) ([]T, error) {
	var all []T
	query.Set("limit", "100")
	for range maxPages {
		var got page[T]
		if err := c.get(path, query, &got); err != nil {
			return nil, err
		}
		all = append(all, got.Data...)
		if !got.HasMore || len(got.Data) == 0 {
			return all, nil
		}
		last := id(got.Data[len(got.Data)-1])
		if last == "" || last == query.Get("starting_after") {
			return nil, fmt.Errorf("stripe: %s: the list does not advance past %q", path, last)
		}
		query.Set("starting_after", last)
	}
	return nil, fmt.Errorf("stripe: %s: more than %d pages; stopping", path, maxPages)
}
