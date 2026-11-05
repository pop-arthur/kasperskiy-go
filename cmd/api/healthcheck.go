package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"
)

func runHealthcheck(args []string) int {
	if len(args) != 1 || args[0] != "healthcheck" {
		fmt.Fprintln(os.Stderr, "usage: api [healthcheck]")
		return 2
	}
	addr, exists := os.LookupEnv("HTTP_ADDR")
	if !exists {
		addr = "127.0.0.1:8080"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := checkHTTP(ctx, addr); err != nil {
		return 1
	}
	return 0
}

func checkHTTP(ctx context.Context, addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}

	target := url.URL{Scheme: "http", Host: net.JoinHostPort(host, port), Path: "/readyz"}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("API is not ready")
	}
	if response.Header.Get("Content-Type") != "application/json" {
		return errors.New("API did not return JSON")
	}
	bodyBytes, err := io.ReadAll(io.LimitReader(response.Body, 1025))
	if err != nil {
		return err
	}
	if len(bodyBytes) > 1024 {
		return errors.New("invalid readiness response")
	}
	decoder := json.NewDecoder(bytes.NewReader(bodyBytes))
	var body struct {
		Status string `json:"status"`
	}
	if err := decoder.Decode(&body); err != nil {
		return err
	}
	if body.Status != "ready" {
		return errors.New("API is not ready")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid readiness response")
	}
	return nil
}
