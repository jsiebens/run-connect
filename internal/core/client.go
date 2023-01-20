package core

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/hashicorp/yamux"
	"google.golang.org/api/iamcredentials/v1"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
)

func StartClient(ctx context.Context, addr, remote string, idToken, serviceAccount, clientId string) error {
	c := &Client{addr, remote, idToken, serviceAccount, clientId}
	return c.start(ctx)
}

type Client struct {
	addr           string
	remote         string
	idToken        string
	serviceAccount string
	clientId       string
}

func (c *Client) start(ctx context.Context) error {
	token, err := c.getToken(ctx)
	if err != nil {
		return err
	}

	conn, err := c.connect(ctx, token)
	if err != nil {
		return err
	}

	// Setup client side of yamux
	session, err := yamux.Client(conn, nil)
	if err != nil {
		return err
	}

	listen, err := net.Listen("tcp", c.addr)
	if err != nil {
		return err
	}

	go func() {
		for {
			conn, err := listen.Accept()
			if err != nil {
				return
			}
			go func(source net.Conn) {
				defer source.Close()
				target, err := session.Open()
				if err != nil {
					return
				}
				defer target.Close()

				pipe(source, target)
			}(conn)
		}
	}()

	defer func() {
		_ = listen.Close()
		_ = session.Close()
		_ = conn.Close()
	}()

	select {
	case <-ctx.Done():
		return nil
	case <-session.CloseChan():
		return errors.New("disconnected from server")
	}
}

func (c *Client) connect(ctx context.Context, token string) (net.Conn, error) {
	u, err := url.Parse(c.remote)
	if err != nil {
		return nil, err
	}

	tr := &http.Transport{
		ForceAttemptHTTP2: false,
		TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
	}

	connCh := make(chan net.Conn, 1)
	trace := httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) {
			connCh <- info.Conn
		},
	}
	traceCtx := httptrace.WithClientTrace(ctx, &trace)
	req := &http.Request{
		Method: "GET",
		URL:    u,
		Header: http.Header{
			"Upgrade":       []string{"websocket"},
			"Connection":    []string{"upgrade"},
			"Authorization": []string{"Bearer " + token},
		},
	}
	req = req.WithContext(traceCtx)

	resp, err := tr.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusSwitchingProtocols {
		return nil, fmt.Errorf("unexpected HTTP response: %s", resp.Status)
	}

	var switchedConn net.Conn
	select {
	case switchedConn = <-connCh:
	default:
	}
	if switchedConn == nil {
		_ = resp.Body.Close()
		return nil, errors.New("httptrace didn't provide a connection")
	}

	if next := resp.Header.Get("Upgrade"); next != "websocket" {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("server switched to unexpected protocol %q", next)
	}

	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		_ = resp.Body.Close()
		return nil, errors.New("http Transport did not provide a writable body")
	}

	return wrappedConn{switchedConn, rwc}, nil
}

func (c *Client) getToken(ctx context.Context) (string, error) {
	if c.idToken != "" {
		return c.idToken, nil
	}

	if c.serviceAccount != "" {
		audience := c.remote

		if c.clientId != "" {
			audience = c.clientId
		}

		service, err := iamcredentials.NewService(ctx)

		if err != nil {
			log.Fatal(err)
		}

		name := fmt.Sprintf("projects/-/serviceAccounts/%s", c.serviceAccount)
		tokenRequest := &iamcredentials.GenerateIdTokenRequest{
			Audience:     audience,
			IncludeEmail: true,
		}

		at, err := service.Projects.ServiceAccounts.GenerateIdToken(name, tokenRequest).Do()
		if err != nil {
			return "", err
		}

		return at.Token, nil
	}

	return "", errors.New("unable to get token, missing id token or service account ")
}

type wrappedConn struct {
	net.Conn
	rwc io.ReadWriteCloser
}

func (w wrappedConn) Read(bs []byte) (int, error) {
	return w.rwc.Read(bs)
}

func (w wrappedConn) Write(bs []byte) (int, error) {
	return w.rwc.Write(bs)
}

func (w wrappedConn) Close() error {
	return w.rwc.Close()
}
