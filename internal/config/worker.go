package config

import (
	"fmt"

	"github.com/caarlos0/env/v11"
	"github.com/go-playground/validator/v10"
)

const defaultUSDJPYRate = 158.535

type Worker struct {
	DatabaseURL         string   `env:"DATABASE_URL,notEmpty" validate:"required"`
	S3Endpoint          string   `env:"S3_ENDPOINT,notEmpty" validate:"required"`
	S3Bucket            string   `env:"S3_BUCKET,notEmpty" validate:"required"`
	S3AccessKeyID       string   `env:"S3_ACCESS_KEY_ID" validate:"required_with=S3SecretAccessKey"`
	S3SecretAccessKey   string   `env:"S3_SECRET_ACCESS_KEY" validate:"required_with=S3AccessKeyID"`
	EmailEncryptionKeys string   `env:"EMAIL_BODY_ENCRYPTION_KEYS"`
	RabbitMQURL         string   `env:"RABBITMQ_URL"`
	GeminiEnabled       bool     `env:"GEMINI_ENABLED" envDefault:"false"`
	GeminiFreeAPIKey    string   `env:"GEMINI_FREE_API_KEY"`
	GeminiPaidAPIKey    string   `env:"GEMINI_PAID_API_KEY"`
	GeminiFreeProject   string   `env:"GEMINI_FREE_PROJECT_ID"`
	GeminiPaidProject   string   `env:"GEMINI_PAID_PROJECT_ID"`
	GeminiFlashModel    string   `env:"GEMINI_FLASH_MODEL"`
	GeminiLiteModel     string   `env:"GEMINI_LITE_MODEL"`
	GeminiProModel      string   `env:"GEMINI_PRO_MODEL"`
	USDJPYRate          *float64 `env:"USDJPY_RATE" validate:"omitempty,gt=0"`
	GeminiUSDJPYRate    *float64 `env:"GEMINI_USDJPY_RATE" validate:"omitempty,gt=0"`
	GeminiQuotaLimits   string   `env:"GEMINI_QUOTA_LIMITS"`
	USDJPYRateSource    string   `env:"USDJPY_RATE_SOURCE"`
	USDJPYRateAsOf      string   `env:"USDJPY_RATE_AS_OF" validate:"omitempty,datetime=2006-01-02"`
	OCRURL              string   `env:"OCR_URL"`
}

func LoadWorker() (Worker, error) {
	return loadWorker(nil)
}

func (c Worker) Validate() error {
	validate := validator.New(validator.WithRequiredStructEnabled())
	validate.RegisterStructValidation(func(level validator.StructLevel) {
		config := level.Current().Interface().(Worker)
		if config.GeminiEnabled && config.GeminiFreeAPIKey == "" && config.GeminiPaidAPIKey == "" {
			level.ReportError(config.GeminiEnabled, "GeminiEnabled", "GEMINI_ENABLED", "requires_api_key", "")
		}
	}, Worker{})
	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("invalid worker configuration: %w", err)
	}
	if _, err := ParseGeminiQuotaLimits(c.GeminiQuotaLimits); err != nil {
		return fmt.Errorf("invalid worker Gemini quota configuration: %w", err)
	}
	return nil
}

func (c Worker) EffectiveUSDJPYRate() float64 {
	if c.USDJPYRate != nil {
		return *c.USDJPYRate
	}
	if c.GeminiUSDJPYRate != nil {
		return *c.GeminiUSDJPYRate
	}
	return defaultUSDJPYRate
}

func (c Worker) EffectiveUSDJPYRateSource() string {
	if c.USDJPYRateSource != "" {
		return c.USDJPYRateSource
	}
	if c.USDJPYRate != nil || c.GeminiUSDJPYRate != nil {
		return "operator_config"
	}
	return "application_default"
}

func loadWorker(environment map[string]string) (Worker, error) {
	var config Worker
	var err error
	if environment == nil {
		config, err = env.ParseAs[Worker]()
	} else {
		config, err = env.ParseAsWithOptions[Worker](env.Options{Environment: environment})
	}
	if err != nil {
		return Worker{}, fmt.Errorf("parse worker environment: %w", err)
	}
	if err := config.Validate(); err != nil {
		return Worker{}, err
	}
	return config, nil
}
