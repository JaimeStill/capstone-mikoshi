package correlation_test

import (
	"context"
	"maps"
	"testing"
	"uuid"

	"github.com/standards-lab/go-web-sdk"

	"github.com/JaimeStill/capstone-mikoshi/correlation"
)

func TestID_TheRequestsID(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), "req-1")
	if got := correlation.ID(ctx); got != "req-1" {
		t.Errorf("ID = %q, want the request's id req-1", got)
	}
}

func TestID_AFreshUUIDv7OutsideARequest(t *testing.T) {
	for name, ctx := range map[string]context.Context{
		"no request id":    context.Background(),
		"empty request id": web.WithRequestID(context.Background(), ""),
	} {
		t.Run(name, func(t *testing.T) {
			first, second := correlation.ID(ctx), correlation.ID(ctx)
			for _, id := range []string{first, second} {
				u, err := uuid.Parse(id)
				if err != nil {
					t.Fatalf("ID = %q, want a UUID: %v", id, err)
				}
				if version := u[6] >> 4; version != 7 {
					t.Errorf("ID = %q is a version %d UUID, want version 7", id, version)
				}
				if variant := u[8] >> 6; variant != 0b10 {
					t.Errorf("ID = %q has variant bits %02b, want the RFC 9562 variant 10", id, variant)
				}
			}
			if first == second {
				t.Errorf("two calls both returned %q, want a fresh id each", first)
			}
		})
	}
}

func TestRaiseOptions_CarryTheCorrelationIDAlone(t *testing.T) {
	ctx := web.WithRequestID(context.Background(), "req-2")
	got := correlation.RaiseOptions(ctx).Extensions
	want := map[string]string{"correlationid": "req-2"}
	if !maps.Equal(got, want) {
		t.Errorf("Extensions = %v, want %v", got, want)
	}
}
