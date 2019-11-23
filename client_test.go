package proxy_webshare_io_test

import (
	"fmt"
	p "github.com/gadelkareem/proxy.webshare.io"
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	u := os.Getenv("PW_URL")
	if u == "" {
		panic("Please provide a proxy.webshare.io download list URL")
	}
	c, err := p.NewClient(u, nil)
	if err != nil {
		t.Error(err)
		return
	}
	ls, err := c.List()
	if err != nil {
		t.Error(err)
		return
	}

	for ip, u := range ls {
		println(ip, u.String())
	}
	tl := c.Total()
	fmt.Printf("Got %d proxies \n", tl)
	if tl < 100 {
		t.Error("Invalid number of proxies")
	}
}
