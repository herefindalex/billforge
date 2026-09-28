package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"billforge/api/admin"
	"billforge/cmd/lab/adminassets"
	"billforge/lab"
	"github.com/joho/godotenv"
)

func runAdmin(args []string) error {
	envPath := ".env"
	assetsDir := filepath.Join("cmd", "lab", "adminassets", "dist")
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if len(args) < 2 {
			return errors.New("admin flag requires a value")
		}
		switch args[0] {
		case "--env-file":
			envPath = args[1]
		case "--assets-dir":
			assetsDir = args[1]
		default:
			return errors.New("usage: lab admin [--env-file path] [--assets-dir path] commerce.db provider.db [127.0.0.1:8080]")
		}
		args = args[2:]
	}
	if len(args) != 2 && len(args) != 3 {
		return errors.New("usage: lab admin [--env-file path] [--assets-dir path] commerce.db provider.db [127.0.0.1:8080]")
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
		return errors.New("admin must bind an explicit loopback IP address")
	}
	values, err := godotenv.Read(envPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("parse env file: %w", err)
	}
	if err != nil && envPath != ".env" {
		return fmt.Errorf("read env file: %w", err)
	}
	get := func(key string) string {
		if value, ok := os.LookupEnv(key); ok {
			return value
		}
		return values[key]
	}
	assetsFS, embedded := adminassets.Built()
	if !embedded {
		assetsFS = os.DirFS(assetsDir)
	}
	if info, err := fs.Stat(assetsFS, "index.html"); err != nil || info.IsDir() {
		return fmt.Errorf("admin UI build not found at %s; run pnpm --dir web/admin build", assetsDir)
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
	var adminCapabilities []string
	if configured := get("BILLFORGE_ADMIN_CAPABILITIES"); configured != "" {
		adminCapabilities = strings.Split(configured, ",")
		for i := range adminCapabilities {
			adminCapabilities[i] = strings.TrimSpace(adminCapabilities[i])
		}
	}
	apiHandler, err := admin.New(l, admin.Config{Username: get("BILLFORGE_ADMIN_USERNAME"), Password: get("BILLFORGE_ADMIN_PASSWORD"), Capabilities: adminCapabilities})
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusTemporaryRedirect)
	})
	assetsHandler := adminAssets(assetsFS)
	mux.HandleFunc("/admin/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/api/") {
			apiHandler.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		assetsHandler.ServeHTTP(w, r)
	})
	server := &http.Server{
		Addr: addr, Handler: adminHostGuard(addr, mux), ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second,
	}
	fmt.Printf("Billforge Admin listening on http://%s/admin/\n", addr)
	return server.ListenAndServe()
}

func adminHostGuard(expected string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !adminHostMatches(expected, r.Host) {
			http.Error(w, "invalid admin host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func adminHostMatches(expected, actual string) bool {
	if strings.EqualFold(expected, actual) {
		return true
	}
	host, port, err := net.SplitHostPort(expected)
	if err != nil || port != "80" {
		return false
	}
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return strings.EqualFold(actual, host)
}

func adminAssets(filesystem fs.FS) http.Handler {
	files := http.FS(filesystem)
	index, _ := fs.ReadFile(filesystem, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		name := strings.TrimPrefix(r.URL.Path, "/admin/")
		if name == "" {
			name = "index.html"
		}
		if !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		file, err := files.Open(name)
		if err == nil {
			info, statErr := file.Stat()
			_ = file.Close()
			if statErr == nil && !info.IsDir() {
				http.StripPrefix("/admin/", http.FileServer(files)).ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(name, "assets/") || strings.Contains(filepath.Base(name), ".") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(index)
	})
}
