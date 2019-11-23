package proxy_webshare_io

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/cenkalti/backoff"
	"github.com/gadelkareem/cachita"
	h "github.com/gadelkareem/go-helpers"
	"github.com/gadelkareem/quiver"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	Version   = "v1"
	userAgent = "proxy_webshare_io_client/" + Version + " " + runtime.GOOS + " " + runtime.GOARCH
)

type client struct {
	*http.Client
	listUrl   string
	listCache cachita.Cache
	proxiesMu sync.Mutex
	proxies   map[string]*url.URL
}

func NewClient(listUrl string, cl *http.Client) (c *client, err error) {
	if cl == nil {
		cl = http.DefaultClient
	}

	f, err := cachita.NewFileCache("/tmp/proxy_webshare_io", 24*time.Hour, 1*time.Hour)
	if err != nil {
		return nil, err
	}
	return &client{Client: cl, listUrl: listUrl, listCache: f, proxies: make(map[string]*url.URL)}, nil
}

func (c *client) List() (map[string]*url.URL, error) {
	k := fmt.Sprintf("list_%s", c.listUrl)
	err := c.getListCache(k)
	if err != nil && !cachita.IsErrorOk(err) {
		return nil, err
	}
	if len(c.proxies) > 0 {
		return c.proxies, nil
	}

	r, err := c.get(c.listUrl)
	if err != nil {
		return nil, err
	}
	defer r.Body.Close()
	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}

	scanner := bufio.NewScanner(bytes.NewReader(b))

	_, err = h.LiftRLimits()
	h.PanicOnError(err)

	wg := h.NewWgExec(50)
	var l string
	for scanner.Scan() {
		l = scanner.Text()
		if l == "" {
			continue
		}
		wg.Run(func(p ...interface{}) {
			line := p[0].(string)
			ip, u, err := parseProxyLine(line)
			if err != nil {
				fmt.Fprintf(os.Stderr, err.Error()+"\n")
				return
			}
			c.proxiesMu.Lock()
			c.proxies[ip] = u
			c.proxiesMu.Unlock()
		}, l)

	}
	wg.Wait()

	err = c.putListCache(k)
	if err != nil {
		return nil, err
	}

	return c.proxies, nil
}

func (c *client) Type() int {
	return quiver.UseIPv4Proxy
}

func (c *client) Name() string {
	return "proxy.webshare.io"
}

func (c *client) Total() int {
	c.proxiesMu.Lock()
	defer c.proxiesMu.Unlock()
	return len(c.proxies)
}

func (c *client) getListCache(k string) error {
	l := make(map[string]string)
	err := c.listCache.Get(k, &l)
	if err != nil && !cachita.IsErrorOk(err) {
		return err
	}
	if len(l) > 0 {
		for ip, u := range l {
			c.proxies[ip] = h.ParseUrl(u)
		}
	}
	return nil
}

func (c *client) putListCache(k string) error {
	l := make(map[string]string)
	for ip, u := range c.proxies {
		l[ip] = u.String()
	}

	err := c.listCache.Put(k, &l, 0)
	if err != nil {
		return err
	}

	return nil
}

func parseProxyLine(line string) (ipStr string, u *url.URL, err error) {
	s := strings.Split(strings.TrimSpace(line), ":")

	var lu string
	switch len(s) {
	case 4:
		lu = fmt.Sprintf("http://%s:%s@%s:%s", s[2], s[3], s[0], s[1])
	case 2:
		lu = fmt.Sprintf("http://%s:%s", s[0], s[1])
	default:
		return "", nil, fmt.Errorf("invalid proxy line %s", line)
	}

	u, err = url.Parse(lu)
	if err != nil {
		return "", nil, fmt.Errorf("%s parsing line %s URL %s", err, line, lu)
	}
	ipStr = lu
	//ipStr, err = proxyIp(u)
	//if err != nil {
	//	return "", nil, fmt.Errorf("%s getting IP for line %s URL %s", err, line, lu)
	//}

	return
}

func proxyIp(proxyUrl *url.URL) (ip string, err error) {

	transport := &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
	c := &http.Client{Transport: transport}
	c.Timeout = 60 * time.Second

	r, err := retryRequest(
		c,
		http.MethodGet,
		"http://ifconfig.io/ip",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/58.0.3029.81 Safari/537.36",
		nil,
	)
	if err != nil {
		return
	}
	defer r.Body.Close()

	b, err := ioutil.ReadAll(r.Body)
	if err != nil {
		return
	}
	if r.StatusCode != 200 {
		return "", fmt.Errorf("invalid Status Code: %d", r.StatusCode)
	}
	ip = strings.TrimSpace(string(b))
	if !h.IsValidIp(ip) {
		return "", fmt.Errorf("invalid IP: %s", ip)
	}

	return
}
func (c *client) get(u string) (*http.Response, error) {
	return retryRequest(c.Client, http.MethodGet, u, userAgent, nil)
}

func retryRequest(cl *http.Client, method, u, useragent string, body io.Reader) (resp *http.Response, err error) {
	backoff.Retry(func() error {
		resp, err = request(cl, method, u, useragent, body)
		if resp != nil && resp.StatusCode >= http.StatusTooManyRequests {
			return errors.New("try again")
		}
		return nil
	}, backoff.NewExponentialBackOff())
	return
}

func request(cl *http.Client, method, u, useragent string, body io.Reader) (*http.Response, error) {
	r, err := http.NewRequest(method, u, body)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", useragent)

	resp, err := cl.Do(r)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, formatError(resp)
	}

	return resp, nil
}

type apiError struct {
	err        string
	statusCode int
}

func (a apiError) Error() string {
	return a.err
}

func (a apiError) StatusCode() int {
	return a.statusCode
}

func formatError(r *http.Response) error {
	defer r.Body.Close()
	e := new(apiError)

	pl := &struct {
		ErrorMessage string `json:"detail"`
	}{}
	if err := json.NewDecoder(r.Body).Decode(pl); err != nil {
		fmt.Fprintf(os.Stderr, err.Error())
	} else if pl.ErrorMessage != "" {
		e.err = pl.ErrorMessage
	} else {
		e.err = fmt.Sprintf("error statusCode code %d: %s", r.StatusCode, http.StatusText(r.StatusCode))
	}
	e.statusCode = r.StatusCode

	return e
}
