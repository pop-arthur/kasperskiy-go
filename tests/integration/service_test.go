//go:build integration

package integration_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"kaspersky-go-test/internal/domain"
	"kaspersky-go-test/internal/usecase"
)

func TestThreatServiceWithPostgres(t *testing.T) {
	ctx, _, repository := newRepository(t)
	service := usecase.NewThreatService(repository)
	md5 := key(t)
	uppercase := strings.ToUpper(string(md5))
	data := domain.ThreatData{Name: "Trojan.Win32.Generic", Description: ptr("description")}
	created, err := service.Create(ctx, uppercase, data)
	if err != nil {
		t.Fatal(err)
	}
	requireThreat(t, created, md5, data)
	got, err := service.GetByMD5(ctx, string(md5))
	if err != nil || !reflect.DeepEqual(got, created) {
		t.Fatalf("read after create = %+v, %v", got, err)
	}

	updatedData := domain.ThreatData{Name: "Trojan.Win32.Updated"}
	updated, err := service.Update(ctx, uppercase, updatedData)
	if err != nil {
		t.Fatal(err)
	}
	requireThreat(t, updated, md5, updatedData)
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Fatal("service update changed created_at")
	}
	got, err = service.GetByMD5(ctx, uppercase)
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("read after update = %+v, %v", got, err)
	}

	if _, err := service.Create(ctx, uppercase, data); !errors.Is(err, domain.ErrThreatAlreadyExists) {
		t.Fatalf("duplicate create = %v", err)
	}
	got, err = service.GetByMD5(ctx, string(md5))
	if err != nil || !reflect.DeepEqual(got, updated) {
		t.Fatalf("duplicate changed stored row: %+v, %v", got, err)
	}
	absent := string(key(t))
	if _, err := service.GetByMD5(ctx, absent); !errors.Is(err, domain.ErrThreatNotFound) {
		t.Fatalf("missing get = %v", err)
	}
	if _, err := service.Update(ctx, absent, data); !errors.Is(err, domain.ErrThreatNotFound) {
		t.Fatalf("missing update = %v", err)
	}
	if _, err := service.Create(ctx, "invalid", data); !errors.Is(err, domain.ErrInvalidMD5) {
		t.Fatalf("invalid hash = %v", err)
	}
	if _, err := service.Update(ctx, uppercase, domain.ThreatData{Name: "bad\x00name"}); !errors.Is(err, domain.ErrInvalidThreatData) {
		t.Fatalf("invalid write data = %v", err)
	}
}
