package purchase

import (
	"errors"
	"testing"
)

func TestNormalizeCreateCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		command      CreateCommand
		wantOrigin   string
		wantDate     string
		wantMerchant string
		wantErr      error
	}{
		{
			name:       "manual defaults",
			command:    CreateCommand{Merchant: "  Market  ", DatePrecision: "", Origin: ""},
			wantOrigin: manualOrigin, wantDate: "DATE_TIME", wantMerchant: "Market",
		},
		{
			name:       "document defaults to AI origin",
			command:    CreateCommand{SourceDocumentID: new("document")},
			wantOrigin: "AI", wantDate: "DATE_TIME",
		},
		{
			name:    "source reference requires a document",
			command: CreateCommand{SourceReference: new("row-1")},
			wantErr: ErrBadInput,
		},
		{
			name:    "source reference rejects line breaks",
			command: CreateCommand{SourceDocumentID: new("document"), SourceReference: new("row\n1")},
			wantErr: ErrBadInput,
		},
		{
			name:    "invalid date precision",
			command: CreateCommand{DatePrecision: "DATE"},
			wantErr: ErrBadInput,
		},
		{
			name:    "invalid origin",
			command: CreateCommand{Origin: "UNKNOWN_ORIGIN"},
			wantErr: ErrBadInput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, origin, err := normalizeCreateCommand(test.command)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("normalizeCreateCommand() error = %v, want %v", err, test.wantErr)
			}
			if err != nil {
				return
			}
			if origin != test.wantOrigin || got.DatePrecision != test.wantDate || got.Merchant != test.wantMerchant {
				t.Fatalf("normalizeCreateCommand() = (%q, %q, %q), want (%q, %q, %q)", origin, got.DatePrecision, got.Merchant, test.wantOrigin, test.wantDate, test.wantMerchant)
			}
		})
	}
}

func TestNormalizeCreateCommandPreservesExplicitValues(t *testing.T) {
	t.Parallel()

	command := CreateCommand{Merchant: "xxx", DatePrecision: "DATE_ONLY", Origin: "UNKNOWN"}
	got, origin, err := normalizeCreateCommand(command)
	if err != nil {
		t.Fatalf("normalizeCreateCommand() error = %v", err)
	}
	if origin != "UNKNOWN" || got.DatePrecision != "DATE_ONLY" || got.Merchant != "xxx" {
		t.Fatalf("normalizeCreateCommand() = (%q, %q, %q), want (UNKNOWN, DATE_ONLY, xxx)", origin, got.DatePrecision, got.Merchant)
	}
}
