package netutil

import (
	"net"
	"strconv"
	"strings"
)

// DefaultAPIPort is the port clients reach an agent's API on unless told
// otherwise.
const DefaultAPIPort = 8080

// WithAPIPort is host with ":port" when the agent's API listens on another
// port than DefaultAPIPort (QR codes and connect links: clients take
// "host:port" as the API address), and host as is otherwise.
func WithAPIPort(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" || port <= 0 || port == DefaultAPIPort {
		return host
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}
