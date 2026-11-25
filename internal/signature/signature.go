// Package signature parses and verifies the gateway's HMAC webhook signature.
package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var (
	// ErrMalformedHeader reports a header that cannot be parsed.
	ErrMalformedHeader = errors.New("signature: malformed header")
	// ErrTimestampOutOfTolerance reports a timestamp outside the replay window.
	ErrTimestampOutOfTolerance = errors.New("signature: timestamp out of tolerance")
	// ErrSignatureMismatch reports that no provided signature matched.
	ErrSignatureMismatch = errors.New("signature: signature mismatch")
)

// Parsed is a decoded signature header. Signatures holds every v1 value so a
// secret rotation can accept more than one key at a time.
type Parsed struct {
	Timestamp  int64
	Signatures [][]byte
}

// Parse decodes a header of the form `t=<unix>,v1=<hex>[,v1=<hex>...]`.
// Multiple v1 values are allowed; duplicate timestamps are rejected as
// ambiguous. Unknown schemes are ignored for forward compatibility.
func Parse(header string) (Parsed, error) {
	var parsed Parsed
	haveTimestamp := false

	for _, rawPart := range strings.Split(header, ",") {
		part := strings.TrimSpace(rawPart)
		if part == "" {
			return Parsed{}, fmt.Errorf("%w: empty component", ErrMalformedHeader)
		}

		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return Parsed{}, fmt.Errorf("%w: component without '='", ErrMalformedHeader)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)

		switch key {
		case "t":
			if haveTimestamp {
				return Parsed{}, fmt.Errorf("%w: duplicate timestamp", ErrMalformedHeader)
			}
			timestamp, err := strconv.ParseInt(value, 10, 64)
			if err != nil || timestamp <= 0 {
				return Parsed{}, fmt.Errorf("%w: invalid timestamp", ErrMalformedHeader)
			}
			parsed.Timestamp = timestamp
			haveTimestamp = true
		case "v1":
			if value == "" {
				return Parsed{}, fmt.Errorf("%w: empty v1", ErrMalformedHeader)
			}
			sig, err := hex.DecodeString(value)
			if err != nil {
				return Parsed{}, fmt.Errorf("%w: invalid v1 hex", ErrMalformedHeader)
			}
			parsed.Signatures = append(parsed.Signatures, sig)
		}
	}

	if !haveTimestamp {
		return Parsed{}, fmt.Errorf("%w: missing timestamp", ErrMalformedHeader)
	}
	if len(parsed.Signatures) == 0 {
		return Parsed{}, fmt.Errorf("%w: missing v1 signature", ErrMalformedHeader)
	}
	return parsed, nil
}

// Verify checks the signature header against the raw request body. The signed
// payload is `<timestamp>.<body>` and every comparison runs in constant time.
// A timestamp outside the symmetric tolerance window (past or future) is a
// rejection, not a warning.
func Verify(secret []byte, header string, body []byte, now time.Time, tolerance time.Duration) error {
	parsed, err := Parse(header)
	if err != nil {
		return err
	}

	delta := now.Sub(time.Unix(parsed.Timestamp, 0))
	if delta > tolerance || delta < -tolerance {
		return fmt.Errorf("%w: outside %s window", ErrTimestampOutOfTolerance, tolerance)
	}

	expected := sign(secret, parsed.Timestamp, body)
	var matched bool
	for _, sig := range parsed.Signatures {
		matched = hmac.Equal(expected, sig) || matched
	}
	if !matched {
		return ErrSignatureMismatch
	}
	return nil
}

// Sign returns the v1 HMAC (SHA-256) for a timestamp and raw body. It is used
// by the demo endpoint to sign samples server-side; callers must never send the
// secret to a client.
func Sign(secret []byte, timestamp int64, body []byte) []byte {
	return sign(secret, timestamp, body)
}

func sign(secret []byte, timestamp int64, body []byte) []byte {
	message := make([]byte, 0, 20+len(body))
	message = strconv.AppendInt(message, timestamp, 10)
	message = append(message, '.')
	message = append(message, body...)

	mac := hmac.New(sha256.New, secret)
	mac.Write(message)
	return mac.Sum(nil)
}
