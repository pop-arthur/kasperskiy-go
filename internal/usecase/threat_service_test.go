package usecase_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"kaspersky-go-test/internal/domain"
	"kaspersky-go-test/internal/usecase"
)

const canonicalMD5 = "d41d8cd98f00b204e9800998ecf8427e"

type operation struct {
	name  string
	write bool
	call  func(*usecase.ThreatService, context.Context, string, domain.ThreatData) (domain.Threat, error)
}

var operations = []operation{
	{"get", false, func(service *usecase.ThreatService, ctx context.Context, md5 string, _ domain.ThreatData) (domain.Threat, error) {
		return service.GetByMD5(ctx, md5)
	}},
	{"create", true, (*usecase.ThreatService).Create},
	{"update", true, (*usecase.ThreatService).Update},
}

type repositoryCall struct {
	ctx  context.Context
	md5  domain.MD5
	data domain.ThreatData
}

type repositoryFake struct {
	t        *testing.T
	expected string
	calls    []repositoryCall
	result   domain.Threat
	err      error
	onCall   func()
}

func (repo *repositoryFake) invoke(method string, ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error) {
	repo.t.Helper()
	if method != repo.expected {
		repo.t.Fatalf("unexpected repository call %s; expected %q", method, repo.expected)
	}
	repo.calls = append(repo.calls, repositoryCall{ctx, md5, data})
	if repo.onCall != nil {
		repo.onCall()
	}
	return repo.result, repo.err
}

func (repo *repositoryFake) GetByMD5(ctx context.Context, md5 domain.MD5) (domain.Threat, error) {
	return repo.invoke("get", ctx, md5, domain.ThreatData{})
}

func (repo *repositoryFake) Create(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error) {
	return repo.invoke("create", ctx, md5, data)
}

func (repo *repositoryFake) Update(ctx context.Context, md5 domain.MD5, data domain.ThreatData) (domain.Threat, error) {
	return repo.invoke("update", ctx, md5, data)
}

func fixtureThreat() domain.Threat {
	description := "stored description"
	return domain.Threat{
		MD5: canonicalMD5, Name: "stored name", Description: &description,
		CreatedAt: time.Date(2026, 11, 5, 10, 0, 0, 123456000, time.UTC),
		UpdatedAt: time.Date(2026, 11, 5, 11, 0, 0, 654321000, time.UTC),
	}
}

func requireError(t *testing.T, got domain.Threat, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("error = %v; want errors.Is(%v)", err, want)
	}
	if !reflect.DeepEqual(got, domain.Threat{}) {
		t.Fatalf("result with error must be zero: %+v", got)
	}
}

func TestThreatServiceNormalizesAndDelegates(t *testing.T) {
	for _, op := range operations {
		for _, raw := range []string{canonicalMD5, strings.ToUpper(canonicalMD5), "D41d8cD98f00b204e9800998ecf8427E"} {
			t.Run(op.name+"/"+raw, func(t *testing.T) {
				repo := &repositoryFake{t: t, result: fixtureThreat()}
				service := usecase.NewThreatService(repo)
				repo.expected = op.name
				type contextKey struct{}
				parent := context.WithValue(context.Background(), contextKey{}, "trace-value")
				ctx, cancel := context.WithTimeout(parent, time.Minute)
				defer cancel()
				description := "input description"
				data := domain.ThreatData{Name: "input name", Description: &description}
				got, err := op.call(service, ctx, raw, data)
				if err != nil || !reflect.DeepEqual(got, repo.result) {
					t.Fatalf("result = %+v, %v; want unmodified repository result", got, err)
				}
				if len(repo.calls) != 1 {
					t.Fatalf("repository calls = %d; want 1", len(repo.calls))
				}
				call := repo.calls[0]
				if call.ctx != ctx || call.md5 != domain.MD5(canonicalMD5) {
					t.Fatalf("repository received a different context or hash: %+v", call)
				}
				if op.write && !reflect.DeepEqual(call.data, data) {
					t.Fatalf("repository data changed: %+v", call.data)
				}
			})
		}
	}
}

func TestThreatServiceRejectsInvalidMD5BeforeRepository(t *testing.T) {
	for _, op := range operations {
		for _, raw := range []string{"", "short", strings.Repeat("g", 32), canonicalMD5 + " ", canonicalMD5 + "\n"} {
			t.Run(op.name+"/"+raw, func(t *testing.T) {
				repo := &repositoryFake{t: t}
				got, err := op.call(usecase.NewThreatService(repo), context.Background(), raw, domain.ThreatData{Name: "\x00"})
				requireError(t, got, err, domain.ErrInvalidMD5)
			})
		}
	}
}

func TestThreatServiceValidatesWritesBeforeRepository(t *testing.T) {
	badDescription := "bad\x00text"
	for _, op := range operations {
		if !op.write {
			continue
		}
		for _, data := range []domain.ThreatData{
			{Name: "bad\x00name"},
			{Name: "name", Description: &badDescription},
		} {
			t.Run(op.name+"/"+data.Name, func(t *testing.T) {
				repo := &repositoryFake{t: t}
				got, err := op.call(usecase.NewThreatService(repo), context.Background(), canonicalMD5, data)
				requireError(t, got, err, domain.ErrInvalidThreatData)
			})
		}
	}
}

func TestThreatServicePreservesWriteData(t *testing.T) {
	empty, unicode := "", " описание\nугрозы "
	for _, op := range operations {
		if !op.write {
			continue
		}
		for _, data := range []domain.ThreatData{
			{},
			{Name: " \n "},
			{Name: "Угроза", Description: &unicode},
			{Name: "name", Description: &empty},
		} {
			t.Run(op.name+"/"+data.Name, func(t *testing.T) {
				repo := &repositoryFake{t: t, expected: op.name, result: fixtureThreat()}
				_, err := op.call(usecase.NewThreatService(repo), context.Background(), canonicalMD5, data)
				if err != nil || len(repo.calls) != 1 {
					t.Fatalf("operation = %v, calls = %d", err, len(repo.calls))
				}
				if !reflect.DeepEqual(repo.calls[0].data, data) {
					t.Fatalf("write data modified: %+v; want %+v", repo.calls[0].data, data)
				}
			})
		}
	}
}

type storageError struct{ code int }

func (err *storageError) Error() string { return "test storage failure" }

func TestThreatServicePreservesRepositoryErrors(t *testing.T) {
	failure := &storageError{code: 123}
	for _, op := range operations {
		causes := []error{fmt.Errorf("storage operation: %w", failure), context.Canceled, context.DeadlineExceeded}
		if op.name == "create" {
			causes = append(causes, domain.ErrThreatAlreadyExists)
		} else {
			causes = append(causes, domain.ErrThreatNotFound)
		}
		for _, cause := range causes {
			t.Run(op.name+"/"+cause.Error(), func(t *testing.T) {
				repo := &repositoryFake{t: t, expected: op.name, result: fixtureThreat(), err: cause}
				got, err := op.call(usecase.NewThreatService(repo), context.Background(), canonicalMD5, domain.ThreatData{Name: "name"})
				requireError(t, got, err, cause)
				if len(repo.calls) != 1 {
					t.Fatalf("repository retried: %d calls", len(repo.calls))
				}
				if errors.Is(cause, failure) {
					var actual *storageError
					if !errors.As(err, &actual) || actual != failure {
						t.Fatal("technical error type/cause was lost")
					}
				}
			})
		}
	}
}

func TestThreatServiceChecksContextBeforeValidation(t *testing.T) {
	for _, op := range operations {
		for _, expired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/expired=%v", op.name, expired), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				want := context.Canceled
				if expired {
					cancel()
					ctx, cancel = context.WithDeadline(context.Background(), time.Unix(1, 0))
					want = context.DeadlineExceeded
				} else {
					cancel()
				}
				defer cancel()
				repo := &repositoryFake{t: t}
				got, err := op.call(usecase.NewThreatService(repo), ctx, "bad hash", domain.ThreatData{Name: "\x00"})
				requireError(t, got, err, want)
			})
		}
	}
}

func TestThreatServiceDoesNotOverrideRepositoryResultWithLateCancellation(t *testing.T) {
	failure := errors.New("repository failure")
	for _, op := range operations {
		for _, cause := range []error{nil, failure} {
			t.Run(fmt.Sprintf("%s/error=%v", op.name, cause), func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				repo := &repositoryFake{t: t, expected: op.name, result: fixtureThreat(), err: cause, onCall: cancel}
				got, err := op.call(usecase.NewThreatService(repo), ctx, canonicalMD5, domain.ThreatData{})
				if cause == nil {
					if err != nil || !reflect.DeepEqual(got, repo.result) {
						t.Fatalf("successful repository result was replaced: %+v, %v", got, err)
					}
				} else {
					requireError(t, got, err, cause)
				}
				if len(repo.calls) != 1 {
					t.Fatalf("unexpected retry: %d calls", len(repo.calls))
				}
			})
		}
	}
}
