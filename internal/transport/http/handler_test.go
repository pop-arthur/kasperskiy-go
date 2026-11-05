package httptransport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"kaspersky-go-test/internal/domain"
	"kaspersky-go-test/internal/usecase"
)

const hash = "d41d8cd98f00b204e9800998ecf8427e"

type serviceFake struct {
	call func(string, context.Context, string, domain.ThreatData) (domain.Threat, error)
}

func (fake serviceFake) GetByMD5(ctx context.Context, md5 string) (domain.Threat, error) {
	return fake.call("get", ctx, md5, domain.ThreatData{})
}
func (fake serviceFake) Create(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error) {
	return fake.call("create", ctx, md5, data)
}
func (fake serviceFake) Update(ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error) {
	return fake.call("update", ctx, md5, data)
}

func logger(writer io.Writer) *slog.Logger { return slog.New(slog.NewJSONHandler(writer, nil)) }

func noService(t *testing.T) serviceFake {
	t.Helper()
	return serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
		t.Error("unexpected service call")
		return domain.Threat{}, errors.New("unexpected service call")
	}}
}

func storedThreat(description *string) domain.Threat {
	return domain.Threat{
		MD5: hash, Name: "", Description: description,
		CreatedAt: time.Date(2026, 11, 5, 13, 0, 0, 123000000, time.FixedZone("MSK", 10800)),
		UpdatedAt: time.Date(2026, 11, 5, 14, 0, 0, 456000000, time.FixedZone("MSK", 10800)),
	}
}

func request(method, path, body string) *http.Request {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	return r
}

func jsonObject(t *testing.T, recorder *httptest.ResponseRecorder, status int) map[string]json.RawMessage {
	t.Helper()
	if recorder.Code != status || recorder.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("status=%d, headers=%v, body=%s; want %d JSON", recorder.Code, recorder.Header(), recorder.Body, status)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result == nil {
		t.Fatalf("invalid response JSON: %s, %v", recorder.Body, err)
	}
	return result
}

func requireErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, status int) {
	t.Helper()
	object := jsonObject(t, recorder, status)
	if len(object) != 2 {
		t.Fatalf("error must have exactly message/details: %s", recorder.Body)
	}
	for _, key := range []string{"message", "details"} {
		raw, ok := object[key]
		var text string
		if !ok || string(raw) == "null" || json.Unmarshal(raw, &text) != nil {
			t.Fatalf("missing string error field %s: %s", key, recorder.Body)
		}
	}
}

func TestHandlersDelegateAndSerialize(t *testing.T) {
	for _, method := range []string{"GET", "POST", "PUT"} {
		for _, description := range []*string{nil, pointer("")} {
			t.Run(fmt.Sprintf("%s/description=%v", method, description), func(t *testing.T) {
				path := "/info/" + strings.ToUpper(hash)
				operation, status := "get", 200
				body := `{"name":"", "description":"", "md5":false,"created_at":[],"extra":null}`
				if method == "POST" {
					path, operation, status = "/info", "create", 201
					body = `{"md5":"` + strings.ToUpper(hash) + `","name":"", "description":"", "updated_at":{},"extra":null}`
				} else if method == "PUT" {
					operation = "update"
				}
				r := request(method, path, body)
				calls := 0
				result := storedThreat(description)
				service := serviceFake{call: func(op string, ctx context.Context, md5 string, data domain.ThreatData) (domain.Threat, error) {
					calls++
					if op != operation || ctx != r.Context() || md5 != strings.ToUpper(hash) {
						t.Errorf("wrong service arguments: %s, %s", op, md5)
					}
					if method != "GET" && (data.Name != "" || data.Description == nil || *data.Description != "") {
						t.Errorf("write data changed: %+v", data)
					}
					return result, nil
				}}
				recorder := httptest.NewRecorder()
				NewHandler(service, logger(io.Discard), 4096).ServeHTTP(recorder, r)
				object := jsonObject(t, recorder, status)
				if calls != 1 || string(object["name"]) != `""` || string(object["md5"]) != `"`+hash+`"` {
					t.Fatalf("unexpected response/calls: %s, %d", recorder.Body, calls)
				}
				_, hasDescription := object["description"]
				if hasDescription != (description != nil) || (hasDescription && string(object["description"]) != `""`) {
					t.Fatalf("NULL/empty description lost: %s", recorder.Body)
				}
				for _, field := range []string{"created_at", "updated_at"} {
					var value string
					if err := json.Unmarshal(object[field], &value); err != nil || !strings.HasSuffix(value, "Z") {
						t.Fatalf("date is not UTC: %s", object[field])
					}
					parsed, err := time.Parse(time.RFC3339Nano, value)
					want := result.CreatedAt
					if field == "updated_at" {
						want = result.UpdatedAt
					}
					if err != nil || !parsed.Equal(want) {
						t.Fatalf("date changed: %s", value)
					}
				}
			})
		}
	}
}

func TestInvalidRequestsDoNotReachService(t *testing.T) {
	cases := []struct{ name, body, only string }{
		{"empty", "", ""}, {"whitespace", " \n ", ""}, {"null", "null", ""},
		{"array", "[]", ""}, {"scalar", "true", ""}, {"truncated", `{"name":`, ""},
		{"two objects", `{} {}`, ""}, {"trailing scalar", `{} true`, ""}, {"trailing junk", `{}x`, ""},
		{"missing name", `{"md5":"` + hash + `"}`, ""},
		{"null name", `{"md5":"` + hash + `","name":null}`, ""},
		{"number name", `{"md5":"` + hash + `","name":1}`, ""},
		{"object name", `{"md5":"` + hash + `","name":{}}`, ""},
		{"null description", `{"md5":"` + hash + `","name":"","description":null}`, ""},
		{"array description", `{"md5":"` + hash + `","name":"","description":[]}`, ""},
		{"case sensitive name", `{"md5":"` + hash + `","Name":""}`, ""},
		{"missing md5", `{"name":""}`, "POST"},
		{"case sensitive md5", `{"MD5":"` + hash + `","name":""}`, "POST"},
		{"null md5", `{"md5":null,"name":""}`, "POST"},
		{"number md5", `{"md5":123,"name":""}`, "POST"},
		{"invalid UTF8", "{\"name\":\"\xff\"}", ""},
		{"duplicate final null", `{"md5":"` + hash + `","name":"ok","name":null}`, ""},
	}
	for _, method := range []string{"POST", "PUT"} {
		for _, test := range cases {
			if test.only != "" && test.only != method {
				continue
			}
			t.Run(method+"/"+test.name, func(t *testing.T) {
				path := "/info"
				if method == "PUT" {
					path += "/" + hash
				}
				recorder := httptest.NewRecorder()
				NewHandler(noService(t), logger(io.Discard), 4096).ServeHTTP(recorder, request(method, path, test.body))
				requireErrorResponse(t, recorder, 400)
			})
		}
	}
}

func TestDecoderPreservesOptionalAndDuplicateFields(t *testing.T) {
	for _, test := range []struct {
		body        string
		description *string
		name        string
	}{
		{`{"name":" \n ","md5":{"ignored":true}}`, nil, " \n "},
		{`{"name":"","description":""}`, pointer(""), ""},
		{`{"name":"Угроза","description":" описание "}`, pointer(" описание "), "Угроза"},
		{`{"name":null,"name":"last","unknown":null,"unknown":false}`, nil, "last"},
	} {
		t.Run(test.body, func(t *testing.T) {
			calls := 0
			service := serviceFake{call: func(op string, _ context.Context, md5 string, data domain.ThreatData) (domain.Threat, error) {
				calls++
				if op != "update" || md5 != hash || data.Name != test.name || !reflect.DeepEqual(data.Description, test.description) {
					t.Errorf("bad decode: %+v", data)
				}
				return storedThreat(nil), nil
			}}
			r := request("PUT", "/info/"+hash, test.body)
			recorder := httptest.NewRecorder()
			NewHandler(service, logger(io.Discard), 4096).ServeHTTP(recorder, r)
			jsonObject(t, recorder, 200)
			if calls != 1 {
				t.Fatal("expected one service call")
			}
		})
	}
}

func TestMediaTypesAndBodyLimits(t *testing.T) {
	body := `{"md5":"` + hash + `","name":""}`
	for _, test := range []struct {
		name, contentType, encoding string
		limit                       int64
		chunked                     bool
		status                      int
	}{
		{"json", "application/json", "", 4096, false, 201},
		{"charset", "Application/JSON; charset=UTF-8", "", 4096, false, 201},
		{"other parameter", "application/json; profile=example", "identity", 4096, false, 201},
		{"missing", "", "", 4096, false, 400},
		{"other type", "text/plain", "", 4096, false, 400},
		{"invalid type", "not a type", "", 4096, false, 400},
		{"other charset", "application/json; charset=latin1", "", 4096, false, 400},
		{"compressed", "application/json", "gzip", 4096, false, 400},
		{"exact limit", "application/json", "", int64(len(body)), false, 201},
		{"too large", "application/json", "", int64(len(body) - 1), false, 400},
		{"chunked too large", "application/json", "", int64(len(body) - 1), true, 400},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := noService(t)
			if test.status == 201 {
				service = serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
					return storedThreat(nil), nil
				}}
			}
			r := request("POST", "/info", body)
			r.Header.Set("Content-Type", test.contentType)
			r.Header.Set("Content-Encoding", test.encoding)
			if test.chunked {
				r.ContentLength = -1
				r.TransferEncoding = []string{"chunked"}
			}
			recorder := httptest.NewRecorder()
			NewHandler(service, logger(io.Discard), test.limit).ServeHTTP(recorder, r)
			if test.status == 400 {
				requireErrorResponse(t, recorder, 400)
			} else {
				jsonObject(t, recorder, 201)
			}
		})
	}
}

func TestServiceErrorMapping(t *testing.T) {
	for _, test := range []struct {
		method string
		cause  error
		status int
	}{
		{"GET", domain.ErrInvalidMD5, 400}, {"PUT", domain.ErrInvalidThreatData, 400},
		{"GET", domain.ErrThreatNotFound, 404}, {"PUT", domain.ErrThreatNotFound, 404},
		{"POST", domain.ErrThreatAlreadyExists, 409},
		{"POST", domain.ErrThreatNotFound, 500}, {"GET", domain.ErrThreatAlreadyExists, 500},
		{"GET", context.Canceled, 500}, {"GET", context.DeadlineExceeded, 500},
		{"GET", errors.New("database unavailable"), 500},
	} {
		t.Run(fmt.Sprintf("%s/%v", test.method, test.cause), func(t *testing.T) {
			var logs bytes.Buffer
			service := serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
				return domain.Threat{}, fmt.Errorf("internal diagnostics: %w", test.cause)
			}}
			path := "/info/" + hash
			if test.method == "POST" {
				path = "/info"
			}
			recorder := httptest.NewRecorder()
			NewHandler(service, logger(&logs), 4096).ServeHTTP(recorder, request(test.method, path, `{"md5":"`+hash+`","name":""}`))
			requireErrorResponse(t, recorder, test.status)
			if strings.Contains(recorder.Body.String(), "internal diagnostics") || strings.Contains(recorder.Body.String(), "database unavailable") {
				t.Fatal("internal error exposed")
			}
			wantErrors := 0
			if test.status == 500 {
				wantErrors = 1
			}
			if got := strings.Count(logs.String(), `"level":"ERROR"`); got != wantErrors {
				t.Fatalf("error logs=%d, want %d: %s", got, wantErrors, &logs)
			}
		})
	}
}

func TestRoutingAndHEAD(t *testing.T) {
	handler := NewHandler(noService(t), logger(io.Discard), 4096)
	for _, test := range []struct {
		method, path string
		status       int
		allow        string
	}{
		{"GET", "/missing", 404, ""}, {"GET", "/info/", 404, ""}, {"GET", "/info/" + hash + "/", 404, ""},
		{"GET", "/info", 405, "POST"}, {"DELETE", "/info/" + hash, 405, "GET, PUT"}, {"OPTIONS", "/info", 405, "POST"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request(test.method, test.path, ""))
			requireErrorResponse(t, recorder, test.status)
			if recorder.Header().Get("Allow") != test.allow {
				t.Fatal("incorrect Allow")
			}
		})
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	response, err := server.Client().Head(server.URL + "/info/" + hash)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || len(body) != 0 || response.StatusCode != 405 || response.Header.Get("Allow") != "GET, PUT" {
		t.Fatalf("HEAD response: %d, %s, %v", response.StatusCode, body, err)
	}
}

func TestRequestContextAndLateCancellation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(fmt.Sprintf("expired=%v", expired), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			if expired {
				cancel()
				ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
			} else {
				cancel()
			}
			defer cancel()
			recorder := httptest.NewRecorder()
			NewHandler(noService(t), logger(io.Discard), 4096).ServeHTTP(recorder, request("GET", "/info/"+hash, "").WithContext(ctx))
			if expired {
				requireErrorResponse(t, recorder, 500)
			} else if recorder.Body.Len() != 0 || len(recorder.Header()) != 0 {
				t.Fatal("attempted response to canceled request")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service := serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
		cancel()
		return storedThreat(nil), nil
	}}
	recorder := httptest.NewRecorder()
	NewHandler(service, logger(io.Discard), 4096).ServeHTTP(recorder, request("GET", "/info/"+hash, "").WithContext(ctx))
	jsonObject(t, recorder, 200)
}

// Verify route matching delegates invalid MD5 to the real service, not a 404 fallback.
func TestInvalidHashThroughRealService(t *testing.T) {
	service := usecase.NewThreatService(unusedRepository{t: t})
	recorder := httptest.NewRecorder()
	NewHandler(service, logger(io.Discard), 4096).ServeHTTP(recorder, request("GET", "/info/bad", ""))
	requireErrorResponse(t, recorder, 400)
}

func TestPanicIsRecoveredWithoutLeakingPayload(t *testing.T) {
	var logs bytes.Buffer
	service := serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
		panic("sensitive-body")
	}}
	recorder := httptest.NewRecorder()
	NewHandler(service, logger(&logs), 4096).ServeHTTP(recorder, request("GET", "/info/"+hash, ""))
	requireErrorResponse(t, recorder, 500)
	if strings.Contains(recorder.Body.String(), "sensitive-body") || strings.Contains(logs.String(), "sensitive-body") {
		t.Fatal("panic payload exposed")
	}
	if strings.Count(logs.String(), `"level":"ERROR"`) != 1 || strings.Count(logs.String(), `"HTTP request completed"`) != 1 {
		t.Fatalf("unexpected logs: %s", &logs)
	}
}

func TestHandlerConcurrentRequests(t *testing.T) {
	var calls atomic.Int32
	service := serviceFake{call: func(string, context.Context, string, domain.ThreatData) (domain.Threat, error) {
		calls.Add(1)
		return storedThreat(nil), nil
	}}
	handler := NewHandler(service, logger(io.Discard), 4096)
	done := make(chan int, 8)
	for range 8 {
		go func() {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, request("GET", "/info/"+hash, ""))
			done <- r.Code
		}()
	}
	for range 8 {
		if status := <-done; status != 200 {
			t.Fatalf("status=%d", status)
		}
	}
	if calls.Load() != 8 {
		t.Fatal("requests lost")
	}
}

func pointer(value string) *string { return &value }

// The real service must reject invalid input without contacting this repository.
type unusedRepository struct{ t *testing.T }

func (repo unusedRepository) unexpected() (domain.Threat, error) {
	repo.t.Error("unexpected repository call for invalid input")
	return domain.Threat{}, errors.New("unexpected repository call")
}
func (repo unusedRepository) GetByMD5(context.Context, domain.MD5) (domain.Threat, error) {
	return repo.unexpected()
}
func (repo unusedRepository) Create(context.Context, domain.MD5, domain.ThreatData) (domain.Threat, error) {
	return repo.unexpected()
}
func (repo unusedRepository) Update(context.Context, domain.MD5, domain.ThreatData) (domain.Threat, error) {
	return repo.unexpected()
}
