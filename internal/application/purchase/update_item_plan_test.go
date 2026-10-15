package purchase

import (
	"errors"
	"testing"
)

func TestValidateUpdateItemCommand(t *testing.T) {
	t.Parallel()

	name := "milk"
	tests := []struct {
		name    string
		command UpdateItemCommand
		wantErr bool
	}{
		{
			name:    "requires a changed field",
			command: UpdateItemCommand{},
			wantErr: true,
		},
		{
			name: "rejects unsupported field",
			command: UpdateItemCommand{
				ChangedFields: []string{"unsupported"},
			},
			wantErr: true,
		},
		{
			name: "rejects duplicate fields",
			command: UpdateItemCommand{
				Name:          &name,
				ChangedFields: []string{itemFieldName, itemFieldName},
			},
			wantErr: true,
		},
		{
			name: "requires the changed value",
			command: UpdateItemCommand{
				ChangedFields: []string{itemFieldName},
			},
			wantErr: true,
		},
		{
			name: "allows clearing a nullable field",
			command: UpdateItemCommand{
				CategoryIDSet: true,
				ChangedFields: []string{itemFieldCategoryID},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			err := validateUpdateItemCommand(test.command)
			if test.wantErr && !errors.Is(err, ErrBadInput) {
				t.Fatalf("validateUpdateItemCommand() error = %v, want ErrBadInput", err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateUpdateItemCommand() error = %v, want nil", err)
			}
		})
	}
}
