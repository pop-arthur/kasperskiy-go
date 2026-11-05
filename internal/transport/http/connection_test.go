package httptransport

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"syscall"
	"testing"
	"time"
)

func TestIncompleteBodyAbortsConnection(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		for _, halfClose := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/halfClose=%v", method, halfClose), func(t *testing.T) {
				server := httptest.NewUnstartedServer(testHandler(noService(t), logger(io.Discard), 4096))
				server.Config.ReadHeaderTimeout = time.Second
				server.Config.ReadTimeout = 100 * time.Millisecond
				server.Config.WriteTimeout = 2 * time.Second
				server.Start()
				defer server.Close()
				conn, err := net.DialTimeout("tcp", server.Listener.Addr().String(), time.Second)
				if err != nil {
					t.Fatal(err)
				}
				defer conn.Close()
				if err := conn.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
					t.Fatal(err)
				}
				path := "/info"
				if method == "PUT" {
					path += "/" + hash
				}
				if _, err := fmt.Fprintf(conn, "%s %s HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\r\nContent-Type: application/json\r\nContent-Length: 50\r\n\r\n{", method, path); err != nil {
					t.Fatal(err)
				}
				if halfClose {
					if err := conn.(*net.TCPConn).CloseWrite(); err != nil {
						t.Fatal(err)
					}
				}
				response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: method})
				if err == nil {
					response.Body.Close()
					t.Fatalf("incomplete request received HTTP %d; want aborted connection", response.StatusCode)
				}
				if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, syscall.ECONNRESET) {
					t.Fatalf("unexpected read error: %v", err)
				}
				response, err = authorizedClient(server).Get(server.URL + "/missing")
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				if response.StatusCode != 404 {
					t.Fatalf("server did not recover: %d", response.StatusCode)
				}
			})
		}
	}
}
