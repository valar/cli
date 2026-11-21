package cmd

import (
	"fmt"
	"io"
	"net/url"
	"sync"
	"time"

	"github.com/valar/cli/config"
)

// tokenExpiryWarning matches when the platform starts sending api.TokenExpiresHeader.
const tokenExpiryWarning = 14 * 24 * time.Hour

// expiryNotice warns about the token's expiry once per invocation.
type expiryNotice struct {
	w    io.Writer
	now  func() time.Time
	once sync.Once
}

func (n *expiryNotice) observe(endpoint string, expires time.Time) {
	left := expires.Sub(n.now())
	if left > tokenExpiryWarning {
		return
	}
	n.once.Do(func() {
		fmt.Fprintf(n.w, "Warning: your Valar token expires %s, at %s. %s\n",
			untilExpiry(left), expires.UTC().Format("2006-01-02 15:04 MST"), renewHint(endpoint))
	})
}

// untilExpiry rounds d to days, hours or minutes.
func untilExpiry(d time.Duration) string {
	unit, size := "minute", time.Minute
	switch {
	case d < time.Minute:
		return "within a minute"
	case d >= 48*time.Hour:
		unit, size = "day", 24*time.Hour
	case d >= 2*time.Hour:
		unit, size = "hour", time.Hour
	}
	n := int((d + size/2) / size)
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("in %d %s", n, unit)
}

// renewHint says where to create a replacement token and how to use it.
func renewHint(endpoint string) string {
	where := "in the Valar console"
	if consoleKnown(endpoint) {
		where = "at https://console.valar.dev/api-tokens/new"
	}
	return "Create a new token " + where + " and use it with 'valar config endpoint set <endpoint> --token <token>' or VALAR_TOKEN."
}

func consoleKnown(endpoint string) bool {
	u, err := url.Parse(endpoint)
	def, _ := url.Parse(config.DefaultEndpoint)
	return err == nil && u.Host == def.Host
}
