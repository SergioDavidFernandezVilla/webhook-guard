package reconcile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sergiodavidfv/webhook-guard/internal/store"
)

func exportCharge(chargeID, state string, amount int64) ExportCharge {
	return ExportCharge{ChargeID: chargeID, State: state, AmountMinor: amount, Currency: "USD"}
}

func localCharge(chargeID string, state store.ChargeState, amount int64) store.Charge {
	return store.Charge{ChargeID: chargeID, State: state, AmountMinor: amount, Currency: "USD"}
}

func TestComputeReport_DetectsMissingLocal(t *testing.T) {
	export := Export{BatchID: "batch", Charges: []ExportCharge{exportCharge("ch_1", "captured", 1000)}}
	report := computeReport(export, nil)

	if got := report.Counts[CategoryMissingLocal]; got != 1 {
		t.Fatalf("missing_local = %d, want 1", got)
	}
	if report.Checked != 1 {
		t.Errorf("checked = %d, want 1", report.Checked)
	}
	if len(report.Diffs) != 1 || report.Diffs[0].ChargeID != "ch_1" {
		t.Errorf("diffs = %#v, want one diff for ch_1", report.Diffs)
	}
	if !report.HasDiffs() {
		t.Error("HasDiffs() = false, want true")
	}
}

func TestComputeReport_DetectsMissingRemote(t *testing.T) {
	export := Export{BatchID: "batch", Charges: []ExportCharge{}}
	local := []store.Charge{localCharge("ch_1", store.StateCaptured, 1000)}

	report := computeReport(export, local)
	if report.Counts[CategoryMissingRemote] != 1 {
		t.Errorf("missing_remote = %d, want 1", report.Counts[CategoryMissingRemote])
	}
	if report.Checked != 1 {
		t.Errorf("checked = %d, want 1", report.Checked)
	}
}

func TestComputeReport_OK(t *testing.T) {
	export := Export{BatchID: "batch", Charges: []ExportCharge{exportCharge("ch_1", "captured", 1000)}}
	local := []store.Charge{localCharge("ch_1", store.StateCaptured, 1000)}

	report := computeReport(export, local)

	if report.Counts[CategoryOK] != 1 {
		t.Errorf("ok = %d, want 1", report.Counts[CategoryOK])
	}
	if len(report.Diffs) != 0 {
		t.Errorf("diffs = %#v, want none", report.Diffs)
	}
	if report.HasDiffs() {
		t.Error("HasDiffs() = true, want false")
	}
}

func TestComputeReport_DetectsAmountMismatch(t *testing.T) {
	export := Export{BatchID: "batch", Charges: []ExportCharge{exportCharge("ch_1", "captured", 1000)}}
	local := []store.Charge{localCharge("ch_1", store.StateCaptured, 900)}

	report := computeReport(export, local)

	if report.Counts[CategoryAmountMismatch] != 1 {
		t.Errorf("amount_mismatch = %d, want 1", report.Counts[CategoryAmountMismatch])
	}
	if report.Counts[CategoryOK] != 0 {
		t.Errorf("ok = %d, want 0", report.Counts[CategoryOK])
	}
	if len(report.Diffs) != 1 || report.Diffs[0].Source != "charges" {
		t.Errorf("diffs = %#v, want one charges diff", report.Diffs)
	}
}

func TestComputeReport_DetectsStateMismatch(t *testing.T) {
	export := Export{BatchID: "batch", Charges: []ExportCharge{exportCharge("ch_1", "captured", 1000)}}
	local := []store.Charge{localCharge("ch_1", store.StateAuthorized, 1000)}

	report := computeReport(export, local)

	if report.Counts[CategoryStateMismatch] != 1 {
		t.Errorf("state_mismatch = %d, want 1", report.Counts[CategoryStateMismatch])
	}
	if report.Counts[CategoryAmountMismatch] != 0 {
		t.Errorf("amount_mismatch = %d, want 0", report.Counts[CategoryAmountMismatch])
	}
	if len(report.Diffs) != 1 || report.Diffs[0].Source != "charges" {
		t.Errorf("diffs = %#v, want one charges diff", report.Diffs)
	}
}

func TestComputeReport_DetectsCurrencyMismatch(t *testing.T) {
	export := Export{BatchID: "batch", Charges: []ExportCharge{
		{ChargeID: "ch_1", State: "captured", AmountMinor: 1000, Currency: "USD"},
	}}
	local := []store.Charge{localCharge("ch_1", store.StateCaptured, 1000)}
	local[0].Currency = "EUR"

	report := computeReport(export, local)

	if report.Counts[CategoryCurrencyMismatch] != 1 {
		t.Errorf("currency_mismatch = %d, want 1", report.Counts[CategoryCurrencyMismatch])
	}
	if report.Counts[CategoryOK] != 0 {
		t.Errorf("ok = %d, want 0", report.Counts[CategoryOK])
	}
	if !report.HasDiffs() {
		t.Error("HasDiffs() = false, want true")
	}
}

func TestComputeReport_EmptyExportIsAllowed(t *testing.T) {
	export := Export{BatchID: "batch", Charges: []ExportCharge{}}
	local := []store.Charge{localCharge("ch_1", store.StateCaptured, 1000)}

	report := computeReport(export, local)

	if report.Counts[CategoryMissingRemote] != 1 {
		t.Errorf("missing_remote = %d, want 1", report.Counts[CategoryMissingRemote])
	}
}

func TestChecksum_AcceptsNonCanonicalFormatting(t *testing.T) {
	// The checksum covers the raw bytes of the charges field, so the gateway's
	// own formatting (spacing, field order inside objects) is preserved.
	raw := `{
  "batch_id": "batch-1",
  "charges": [ { "currency" : "USD" , "charge_id":"ch_1" , "state":"captured", "amount_minor":1000 }
 ]
}`
	var rawExport struct {
		BatchID  string          `json:"batch_id"`
		Checksum string          `json:"checksum"`
		Charges  json.RawMessage `json:"charges"`
	}
	if err := json.Unmarshal([]byte(raw), &rawExport); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	sum := sha256.Sum256(rawExport.Charges)
	want := hex.EncodeToString(sum[:])
	if got := checksumHex(rawExport.Charges); got != want {
		t.Errorf("checksumHex() = %s, want %s", got, want)
	}
	if want != strings.ToLower(want) {
		t.Errorf("expected lowercase hex digest, got %q", want)
	}
}
