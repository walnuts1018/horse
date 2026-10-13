package inputvalidation

import (
	"testing"

	"github.com/walnuts1018/horse/internal/application/apperror"
)

func TestCheckValidatesNonBlankAndMaximumLength(t *testing.T) {
	t.Parallel()

	type command struct {
		Name string `validate:"notblank,max=4"`
	}
	tests := []struct {
		name    string
		input   command
		wantErr bool
	}{
		{name: "valid", input: command{Name: "milk"}},
		{name: "blank", input: command{Name: "  \t"}, wantErr: true},
		{name: "too long", input: command{Name: "milk!"}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := Check(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Check() error = %v, want error %t", err, tt.wantErr)
			}
			if err != nil {
				if kind, ok := apperror.KindOf(err); !ok || kind != apperror.KindBadInput {
					t.Fatalf("Check() error kind = %v, want %v", kind, apperror.KindBadInput)
				}
			}
		})
	}
}
