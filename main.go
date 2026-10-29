// Command md renders markdown to HTML and serves it on localhost.
//
// Usage:
//
//	md README.md        render a single file
//	md docs/            browse and render all markdown files in a folder
//	cat x.md | md       render markdown from stdin
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "md:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on (port 0 picks a free port)")
	noOpen := flag.Bool("no-open", false, "do not open the browser")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [flags] [file.md | folder | -]\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "Without an argument (or with \"-\"), markdown is read from stdin.")
		fmt.Fprintln(flag.CommandLine.Output())
		flag.PrintDefaults()
	}
	flag.Parse()

	if flag.NArg() > 1 {
		flag.Usage()
		return errors.New("too many arguments")
	}

	srv, err := newServerFromArg(flag.Arg(0))
	if err != nil {
		return err
	}
	defer func() { _ = srv.Close() }()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	url := "http://" + ln.Addr().String() + "/"
	fmt.Fprintf(os.Stderr, "Serving %s on %s (Ctrl+C to stop)\n", srv.name, url)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	httpSrv := &http.Server{
		Handler:           srv.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		// Cancels long-lived watch streams on Ctrl+C so Shutdown doesn't wait for them.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	if !*noOpen {
		if err := openBrowser(url); err != nil {
			fmt.Fprintln(os.Stderr, "md: could not open browser:", err)
		}
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// newServerFromArg builds a server for a file, a folder or stdin.
func newServerFromArg(arg string) (*server, error) {
	if arg == "" || arg == "-" {
		if arg == "" && isTerminal(os.Stdin) {
			flag.Usage()
			return nil, errors.New("no input: pass a file, a folder or pipe markdown via stdin")
		}
		src, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("read stdin: %w", err)
		}
		// Relative links/images in stdin markdown resolve against the working directory.
		return newServer(".", "", src, "stdin")
	}

	info, err := os.Stat(arg)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return newServer(arg, "", nil, arg)
	}
	return newServer(filepath.Dir(arg), filepath.Base(arg), nil, arg)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
