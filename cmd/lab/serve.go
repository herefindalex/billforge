package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"billforge/api"
	"billforge/lab"
)

func runServe(args []string) error {
	if len(args) != 2 && len(args) != 3 {
		return errors.New("usage: lab serve commerce.db provider.db [127.0.0.1:8080]")
	}
	if args[0] == args[1] {
		return errors.New("commerce and provider database paths must differ")
	}
	addr := "127.0.0.1:8080"
	if len(args) == 3 {
		addr = args[2]
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return errors.New("lab API must bind to an explicit loopback address")
	}
	for _, path := range args[:2] {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return err
		}
	}
	l, err := lab.Open(args[0], args[1], nil)
	if err != nil {
		return err
	}
	defer l.Close()
	server := &http.Server{Addr: addr, Handler: api.New(l, os.Getenv("BILLFORGE_INTERNAL_TOKEN")), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second}
	fmt.Printf("Billforge v1 API listening on %s\n", addr)
	return server.ListenAndServe()
}
