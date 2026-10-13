package inputvalidation

import (
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/walnuts1018/horse/internal/application/apperror"
)

var inputValidator = newValidator()

func Check(input any) error {
	if err := inputValidator.Struct(input); err != nil {
		return apperror.Wrap(apperror.KindBadInput, fmt.Errorf("invalid input: %w", err))
	}
	return nil
}

func newValidator() *validator.Validate {
	validate := validator.New(validator.WithRequiredStructEnabled())
	if err := validate.RegisterValidation("notblank", func(field validator.FieldLevel) bool {
		return strings.TrimSpace(field.Field().String()) != ""
	}); err != nil {
		panic(fmt.Errorf("register input validation rule: %w", err))
	}
	return validate
}
