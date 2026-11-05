package httptransport

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kaspersky-go-test/internal/domain"
)

func TestEncodingFailureIsJSON500(t *testing.T) {
	var logs bytes.Buffer
	service := serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
		threat := storedThreat(nil)
		threat.CreatedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		return threat, nil
	}}
	recorder := httptest.NewRecorder()
	NewHandler(service, logger(&logs), 4096).ServeHTTP(recorder, request("GET", "/info/"+hash, ""))
	requireErrorResponse(t, recorder, 500)
	if strings.Count(logs.String(), `"level":"ERROR"`) != 1 {
		t.Fatalf("error not logged once: %s", &logs)
	}
}

type failingWriter struct {
	header   http.Header
	statuses []int
	short    bool
}

func (w *failingWriter) Header() http.Header    { return w.header }
func (w *failingWriter) WriteHeader(status int) { w.statuses = append(w.statuses, status) }
func (w *failingWriter) Write(body []byte) (int, error) {
	if w.short {
		return len(body) / 2, nil
	}
	return 0, io.ErrClosedPipe
}

func TestWriteFailureDoesNotSendSecondResponse(t *testing.T) {
	for _, short := range []bool{false, true} {
		var logs bytes.Buffer
		service := serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
			return storedThreat(nil), nil
		}}
		writer := &failingWriter{header: make(http.Header), short: short}
		NewHandler(service, logger(&logs), 4096).ServeHTTP(writer, request("GET", "/info/"+hash, ""))
		if len(writer.statuses) != 1 || writer.statuses[0] != 200 {
			t.Fatalf("second status after write failure: %v", writer.statuses)
		}
		if strings.Count(logs.String(), `"level":"ERROR"`) != 1 {
			t.Fatalf("write failure not logged once: %s", &logs)
		}
	}
}

func TestPanicAfterPartialResponseAbortsStream(t *testing.T) {
	partial := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("prefix"))
		if err := http.NewResponseController(w).Flush(); err != nil {
			panic(err)
		}
		panic("after headers")
	})
	server := httptest.NewServer(requestLogging(logger(io.Discard), recoverPanics(partial)))
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if response.StatusCode != 200 || err == nil || string(body) != "prefix" {
		t.Fatalf("partial response = %d, %q, %v", response.StatusCode, body, err)
	}
}

func TestAbortHandlerIsNotLoggedAsPanic(t *testing.T) {
	var logs bytes.Buffer
	wrapped := requestLogging(logger(&logs), recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })))
	defer func() {
		if value := recover(); value != http.ErrAbortHandler {
			t.Errorf("abort sentinel changed: %v", value)
		}
		if strings.Contains(logs.String(), `"level":"ERROR"`) || !strings.Contains(logs.String(), `"aborted":true`) {
			t.Errorf("abort was logged as panic: %s", &logs)
		}
	}()
	wrapped.ServeHTTP(httptest.NewRecorder(), request("GET", "/", ""))
	t.Fatal("abort was swallowed")
}

type brokenBody struct {
	closed bool
	cancel context.CancelFunc
}

func (body *brokenBody) Read([]byte) (int, error) {
	if body.cancel != nil {
		body.cancel()
	}
	return 0, errors.New("private read error")
}
func (body *brokenBody) Close() error { body.closed = true; return nil }

func TestReadFailureAndCanceledRead(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		r := request("POST", "/info", "").WithContext(ctx)
		body := &brokenBody{}
		if canceled {
			body.cancel = cancel
		}
		r.Body = body
		recorder := httptest.NewRecorder()
		NewHandler(noService(t), logger(io.Discard), 4096).ServeHTTP(recorder, r)
		cancel()
		if !body.closed {
			t.Fatal("request body not closed")
		}
		if canceled {
			if recorder.Body.Len() != 0 {
				t.Fatal("response attempted after canceled read")
			}
		} else {
			requireErrorResponse(t, recorder, 400)
			if strings.Contains(recorder.Body.String(), "private") {
				t.Fatal("read error leaked")
			}
		}
	}
}

func TestAccessLogDoesNotIncludeRequestData(t *testing.T) {
	var logs bytes.Buffer
	service := serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
		return storedThreat(nil), nil
	}}
	r := request("POST", "/info", `{"md5":"`+hash+`","name":"secret-body"}`)
	NewHandler(service, logger(&logs), 4096).ServeHTTP(httptest.NewRecorder(), r)
	if strings.Contains(logs.String(), hash) || strings.Contains(logs.String(), "secret-body") {
		t.Fatalf("request data logged: %s", &logs)
	}
	if !strings.Contains(logs.String(), `"route":"/info"`) || !strings.Contains(logs.String(), `"status":201`) {
		t.Fatalf("missing request summary: %s", &logs)
	}
}

func TestOversizedChunkedBodyThroughServer(t *testing.T) {
	server := httptest.NewServer(NewHandler(noService(t), logger(io.Discard), 32))
	defer server.Close()
	body := `{"md5":"` + hash + `","name":"oversized"}`
	r, err := http.NewRequest("POST", server.URL+"/info", io.NopCloser(strings.NewReader(body)))
	if err != nil {
		t.Fatal(err)
	}
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	r.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	encoded, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 400 || !bytes.Contains(encoded, []byte("size limit")) {
		t.Fatalf("chunked request: %d, %s, %v", response.StatusCode, encoded, err)
	}
}
