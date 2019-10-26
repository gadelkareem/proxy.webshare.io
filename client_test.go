package proxy_webshare_io_test

import (
	"fmt"
	p "github.com/gadelkareem/proxy.webshare.io"
	"os"
	"testing"
)

func TestNewClient(t *testing.T) {
	c, err := p.NewClient(os.Getenv("PW_URL"), nil)
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
	if tl < 100{
		t.Error("Invalid number of proxies")
	}
}
