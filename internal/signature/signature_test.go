package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var testSecret = []byte("whsec_test")

func fixedNow() time.Time {
	return time.Unix(1_700_000_000, 0).UTC()
}

func signHeader(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d.%s", timestamp, body)
	return fmt.Sprintf("t=%d,v1=%x", timestamp, mac.Sum(nil))
}

func TestVerify_Valid(t *testing.T) {
	body := []byte(`{"type":"payment.captured"}`)
	now := fixedNow()
	header := signHeader(testSecret, now.Unix(), body)

	if err := Verify(testSecret, header, body, now, 5*time.Minute); err != nil {
		t.Fatalf("Verify() error = %v, want nil", err)
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	body := []byte(`{"type":"payment.captured"}`)
	now := fixedNow()
	header := signHeader([]byte("whsec_other"), now.Unix(), body)

	err := Verify(testSecret, header, body, now, 5*time.Minute)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("Verify() error = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerify_ExpiredTimestamp(t *testing.T) {
	body := []byte(`{"type":"payment.captured"}`)
	now := fixedNow()
	timestamp := now.Add(-10 * time.Minute).Unix()
	header := signHeader(testSecret, timestamp, body)

	err := Verify(testSecret, header, body, now, 5*time.Minute)
	if !errors.Is(err, ErrTimestampOutOfTolerance) {
		t.Fatalf("Verify() error = %v, want ErrTimestampOutOfTolerance", err)
	}
}

func TestVerify_FutureTimestamp(t *testing.T) {
	body := []byte(`{"type":"payment.captured"}`)
	now := fixedNow()
	timestamp := now.Add(10 * time.Minute).Unix()
	header := signHeader(testSecret, timestamp, body)

	err := Verify(testSecret, header, body, now, 5*time.Minute)
	if !errors.Is(err, ErrTimestampOutOfTolerance) {
		t.Fatalf("Verify() error = %v, want ErrTimestampOutOfTolerance", err)
	}
}

func TestVerify_MalformedHeader(t *testing.T) {
	now := fixedNow()
	validHex := strings.Repeat("ab", sha256.Size)

	cases := []struct {
		name   string
		header string
	}{
		{name: "empty", header: ""},
		{name: "missing timestamp", header: "v1=" + validHex},
		{name: "missing signature", header: fmt.Sprintf("t=%d", now.Unix())},
		{name: "invalid timestamp", header: "t=abc,v1=" + validHex},
		{name: "negative timestamp", header: "t=-1,v1=" + validHex},
		{name: "empty timestamp", header: "t=,v1=" + validHex},
		{name: "empty v1", header: fmt.Sprintf("t=%d,v1=", now.Unix())},
		{name: "invalid v1 hex", header: fmt.Sprintf("t=%d,v1=zz", now.Unix())},
		{name: "component without equals", header: fmt.Sprintf("t=%d,v1", now.Unix())},
		{name: "empty component", header: fmt.Sprintf("t=%d,,v1=%s", now.Unix(), validHex)},
		{name: "duplicate timestamp", header: fmt.Sprintf("t=%d,t=%d,v1=%s", now.Unix(), now.Unix(), validHex)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(tc.header)
			if !errors.Is(err, ErrMalformedHeader) {
				t.Fatalf("Parse() error = %v, want ErrMalformedHeader", err)
			}
		})
	}
}

func TestVerify_MultipleV1OneValid(t *testing.T) {
	body := []byte(`{"type":"payment.captured"}`)
	now := fixedNow()

	mac := hmac.New(sha256.New, testSecret)
	_, _ = fmt.Fprintf(mac, "%d.%s", now.Unix(), body)
	validHex := hex.EncodeToString(mac.Sum(nil))
	wrongHex := strings.Repeat("00", sha256.Size)

	headers := []string{
		fmt.Sprintf("t=%d,v1=%s,v1=%s", now.Unix(), wrongHex, validHex),
		fmt.Sprintf("t=%d,v1=%s,v1=%s", now.Unix(), validHex, wrongHex),
	}
	for _, header := range headers {
		if err := Verify(testSecret, header, body, now, 5*time.Minute); err != nil {
			t.Errorf("Verify(%q) error = %v, want nil", header, err)
		}
	}
}

func TestVerify_BodyTampered(t *testing.T) {
	original := []byte(`{"type":"payment.captured","amount":1000}`)
	tampered := []byte(`{"type":"payment.captured","amount":9999}`)
	now := fixedNow()
	header := signHeader(testSecret, now.Unix(), original)

	err := Verify(testSecret, header, tampered, now, 5*time.Minute)
	if !errors.Is(err, ErrSignatureMismatch) {
		t.Fatalf("Verify() error = %v, want ErrSignatureMismatch", err)
	}
}

func TestVerify_ToleranceBoundary(t *testing.T) {
	body := []byte(`{"type":"payment.captured"}`)
	now := fixedNow()
	tolerance := 5 * time.Minute

	accepted := []time.Time{
		now.Add(-tolerance),
		now.Add(tolerance),
	}
	for _, ts := range accepted {
		header := signHeader(testSecret, ts.Unix(), body)
		if err := Verify(testSecret, header, body, now, tolerance); err != nil {
			t.Errorf("Verify() at boundary %s error = %v, want nil", ts, err)
		}
	}

	rejected := []time.Time{
		now.Add(-tolerance - time.Second),
		now.Add(tolerance + time.Second),
	}
	for _, ts := range rejected {
		header := signHeader(testSecret, ts.Unix(), body)
		err := Verify(testSecret, header, body, now, tolerance)
		if !errors.Is(err, ErrTimestampOutOfTolerance) {
			t.Errorf("Verify() one second past boundary error = %v, want ErrTimestampOutOfTolerance", err)
		}
	}
}

func TestParse_MultipleV1(t *testing.T) {
	now := fixedNow()
	header := fmt.Sprintf("t=%d,v1=aa,v1=bb", now.Unix())

	parsed, err := Parse(header)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if parsed.Timestamp != now.Unix() {
		t.Errorf("Timestamp = %d, want %d", parsed.Timestamp, now.Unix())
	}
	if len(parsed.Signatures) != 2 {
		t.Errorf("len(Signatures) = %d, want 2", len(parsed.Signatures))
	}
}

func TestParse_IgnoresUnknownSchemes(t *testing.T) {
	now := fixedNow()
	header := fmt.Sprintf("t=%d,v0=ff,v1=aa", now.Unix())

	if _, err := Parse(header); err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
}
