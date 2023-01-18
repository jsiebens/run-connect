package core

import (
	"errors"
	"fmt"
	"github.com/hashicorp/yamux"
	"io"
	"net"
	"net/http"
	"time"
)

func StartServer(addr, mode, upstream string) error {
	controlLn, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s := &Server{mode, upstream}

	return http.Serve(controlLn, http.HandlerFunc(s.tunnel))
}

type Server struct {
	mode     string
	upstream string
}

func (s *Server) tunnel(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()

	conn, err := s.acceptHTTP(w, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Setup server side of yamux
	mux, err := yamux.Server(conn, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if s.mode == "proxy" {
		go func() {
			s := &http.Server{Handler: http.HandlerFunc(s.connect)}
			_ = s.Serve(mux)
		}()
	} else {
		go func() {
			for {
				src, err := mux.Accept()
				if err != nil {
					return
				}

				go func(src net.Conn) {
					defer src.Close()
					dst, err := net.DialTimeout("tcp", s.upstream, 10*time.Second)
					if err != nil {
						return
					}
					defer dst.Close()
					pipe(src, dst)
				}(src)
			}
		}()
	}

	select {
	case <-ctx.Done():
	case <-mux.CloseChan():
	}
}

func (s *Server) connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodConnect {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	defer r.Body.Close()

	conn, err := net.DialTimeout("tcp", r.Host, time.Second*10)
	if err != nil {
		http.Error(w, fmt.Sprintf("unable to dial %s, error: %s", r.Host, err.Error()), http.StatusServiceUnavailable)
		return
	}
	defer conn.Close()
	w.WriteHeader(http.StatusOK)

	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "make request over HTTP/1", http.StatusBadRequest)
		return
	}

	reqConn, wbuf, err := hj.Hijack()
	if err != nil {
		http.Error(w, fmt.Sprintf("hijacking client connection: %s", err), http.StatusInternalServerError)
		return
	}
	defer reqConn.Close()
	defer wbuf.Flush()

	pipe(reqConn, conn)
}

func (s *Server) acceptHTTP(w http.ResponseWriter, r *http.Request) (net.Conn, error) {
	next := r.Header.Get("Upgrade")
	if next == "" {
		return nil, errors.New("missing next protocol")
	}
	if next != "websocket" {
		return nil, errors.New("unknown next protocol")
	}

	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("make request over HTTP/1")
	}

	w.Header().Set("Upgrade", "websocket")
	w.Header().Set("Connection", "upgrade")
	w.WriteHeader(http.StatusSwitchingProtocols)

	conn, brw, err := hijacker.Hijack()
	if err != nil {
		return nil, fmt.Errorf("hijacking client connection: %w", err)
	}

	if err := brw.Flush(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("flushing hijacked HTTP buffer: %w", err)
	}

	return conn, nil
}

func pipe(from, to net.Conn) {

	cp := func(c chan bool, dst io.Writer, src io.Reader) {
		_, _ = io.Copy(dst, src)
		c <- true
	}

	closer := make(chan bool, 2)
	go cp(closer, from, to)
	go cp(closer, to, from)
	<-closer
}
