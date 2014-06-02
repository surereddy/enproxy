package main

import (
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"

	"github.com/getlantern/enproxy"
)

func main() {
	enproxyConfig := &enproxy.Config{
		DialProxy: func(addr string) (net.Conn, error) {
			return net.Dial("tcp", os.Args[2])
		},
		NewRequest: func(method string, body io.Reader) (req *http.Request, err error) {
			return http.NewRequest(method, "http://"+os.Args[2]+"/", body)
		},
	}
	httpServer := &http.Server{
		Addr: os.Args[1],
		Handler: &ClientHandler{
			ProxyAddr: os.Args[2],
			Config:    enproxyConfig,
			ReverseProxy: &httputil.ReverseProxy{
				Director: func(req *http.Request) {
					// do nothing
				},
				Transport: &http.Transport{
					Dial: func(network string, addr string) (net.Conn, error) {
						conn := &enproxy.Conn{
							Addr:   addr,
							Config: enproxyConfig,
						}
						err := conn.Connect()
						if err != nil {
							return nil, err
						}
						return conn, nil
					},
				},
			},
		},
	}
	err := httpServer.ListenAndServe()
	if err != nil {
		log.Fatal(err)
	}
}

type ClientHandler struct {
	ProxyAddr    string
	Config       *enproxy.Config
	ReverseProxy *httputil.ReverseProxy
}

func (c *ClientHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	if req.Method == "CONNECT" {
		c.Config.Intercept(resp, req, true)
	} else {
		c.ReverseProxy.ServeHTTP(resp, req)
	}
}
