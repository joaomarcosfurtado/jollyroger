package db_test

import (
	"testing"
	"time"

	wiredb "github.com/joaomarcosfurtado/jollyroger/internal/wire/db"
)

func TestTime_ScanAcceptsBothDialects(t *testing.T) {
	t.Parallel()
	want := time.Date(2026, 9, 27, 12, 0, 0, 123456000, time.UTC)
	brt := time.FixedZone("BRT", -3*60*60)
	for _, src := range []any{
		want,
		want.In(brt), // PostgreSQL drivers may return local time
		"2026-09-27T12:00:00.123456Z",
		[]byte("2026-09-27T12:00:00.123456Z"),
		"2026-09-27T09:00:00.123456-03:00",
	} {
		var got wiredb.Time
		if err := got.Scan(src); err != nil {
			t.Fatalf("Scan(%v): %v", src, err)
		}
		if !got.Time.Equal(want) || got.Time.Location() != time.UTC {
			t.Errorf("Scan(%v) = %v (%v), want %v in UTC", src, got.Time, got.Time.Location(), want)
		}
	}
}

func TestTime_ScanRejectsGarbage(t *testing.T) {
	t.Parallel()
	for _, src := range []any{nil, 42, "yesterday", []byte("2026-13-01T00:00:00Z")} {
		var got wiredb.Time
		if err := got.Scan(src); err == nil {
			t.Errorf("Scan(%v) must fail", src)
		}
	}
}

func TestNullTime_Scan(t *testing.T) {
	t.Parallel()
	var n wiredb.NullTime
	if err := n.Scan(nil); err != nil || n.Valid {
		t.Fatalf("Scan(nil) = %+v, %v; want invalid, nil error", n, err)
	}
	if err := n.Scan("2026-09-27T12:00:00.000000Z"); err != nil || !n.Valid || n.Time.Hour() != 12 {
		t.Fatalf("Scan(text) = %+v, %v", n, err)
	}
	if err := n.Scan(3.5); err == nil {
		t.Fatal("Scan(float) must fail")
	}
}
