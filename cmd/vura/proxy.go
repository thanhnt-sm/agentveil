package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// P2 #17: Consolidated — delegates to the main proxy binary instead of
// duplicating the entire bootstrap logic from cmd/proxy/main.go.
func handleProxy(args []string) {
	if len(args) == 0 || args[0] != "start" {
		fmt.Println("Usage: agentveil proxy start")
		return
	}

	// Find the proxy binary
	proxyBin := findProxyBinary()
	if proxyBin == "" {
		fmt.Fprintln(os.Stderr, "Error: agentveil-proxy binary not found.")
		fmt.Fprintln(os.Stderr, "Build it: go build -o ~/.agentveil/bin/agentveil-proxy ./cmd/proxy/")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "🛡️  Starting Agent Veil proxy via %s\n", proxyBin)

	cmd := exec.Command(proxyBin)
	cmd.Env = os.Environ()
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "Error starting proxy: %v\n", err)
		os.Exit(1)
	}

	// Forward signals to child
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		cmd.Process.Signal(sig)
	}()

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "Proxy error: %v\n", err)
		os.Exit(1)
	}
}

func findProxyBinary() string {
	// Check common locations
	candidates := []string{
		os.ExpandEnv("$HOME/.agentveil/bin/agentveil-proxy"),
		"./agentveil-proxy",
	}

	// Also check if it's in PATH
	if path, err := exec.LookPath("agentveil-proxy"); err == nil {
		return path
	}

	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
