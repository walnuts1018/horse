package ingestion

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"google.golang.org/genai"
)

func generateGeminiContent(ctx context.Context, apiKey, baseURL string, httpClient *http.Client, timeout time.Duration, model string, contents []*genai.Content, config *genai.GenerateContentConfig, reserveInputTokens func(context.Context, int64) (func(context.Context, int64) error, error)) (*genai.GenerateContentResponse, geminiUsage, error) {
	capture := new(geminiResponseCapture)
	client, err := newGeminiClient(ctx, apiKey, baseURL, httpClient, timeout, capture)
	if err != nil {
		return nil, geminiUsage{}, &GeminiAPIError{Kind: GeminiFailureRejected, Code: "client_initialization_failed"}
	}
	var settleQuota func(context.Context, int64) error
	if reserveInputTokens != nil {
		countedTokens, err := client.Models.CountTokens(ctx, model, contents, nil)
		if err != nil {
			if apiErr, ok := errors.AsType[genai.APIError](err); ok {
				return nil, geminiUsage{}, providerError(apiErr, capture)
			}
			if capture.statusCode == http.StatusOK {
				return nil, geminiUsage{}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "malformed_provider_json"}
			}
			return nil, geminiUsage{}, fmt.Errorf("count input tokens: %w", err)
		}
		settleQuota, err = reserveInputTokens(ctx, int64(countedTokens.TotalTokens))
		if err != nil {
			return nil, geminiUsage{}, err
		}
	}
	capture.statusCode = 0
	capture.header = nil
	result, err := client.Models.GenerateContent(ctx, model, contents, config)
	if err != nil {
		if apiErr, ok := errors.AsType[genai.APIError](err); ok {
			return nil, geminiUsage{}, providerError(apiErr, capture)
		}
		if capture.statusCode == http.StatusOK {
			return nil, geminiUsage{}, &GeminiAPIError{Kind: GeminiFailureBadResponse, Code: "malformed_provider_json"}
		}
		return nil, geminiUsage{}, &GeminiAPIError{Kind: GeminiFailureTemporary, Code: "transport_error"}
	}
	usage := decodeGeminiUsage(result)
	if settleQuota != nil && usage.available {
		if err := settleQuota(ctx, usage.inputTokens); err != nil {
			return nil, usage, fmt.Errorf("settle Gemini quota reservation: %w", err)
		}
	}
	return result, usage, nil
}

func newGeminiClient(ctx context.Context, apiKey, baseURL string, httpClient *http.Client, timeout time.Duration, capture *geminiResponseCapture) (*genai.Client, error) {
	return genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:     apiKey,
		Backend:    genai.BackendGeminiAPI,
		HTTPClient: geminiHTTPClient(httpClient, timeout, capture),
		HTTPOptions: genai.HTTPOptions{
			BaseURL:      baseURL,
			APIVersion:   "v1beta",
			RetryOptions: &genai.HTTPRetryOptions{Attempts: new(int32(1))},
		},
	})
}

func providerError(response genai.APIError, capture *geminiResponseCapture) *GeminiAPIError {
	classificationText := strings.ToLower(response.Status + " " + response.Message)
	statusCode := response.Code
	if capture.statusCode != 0 {
		statusCode = capture.statusCode
	}
	apiErr := &GeminiAPIError{StatusCode: statusCode, Code: "provider_rejected"}
	for _, detail := range response.Details {
		if reason, ok := detail["reason"].(string); ok {
			classificationText += " " + strings.ToLower(reason)
		}
		if retryDelay, ok := detail["retryDelay"].(string); ok {
			if parsed, err := time.ParseDuration(retryDelay); err == nil {
				apiErr.RetryAfter = max(apiErr.RetryAfter, parsed)
			}
		}
	}
	if value := capture.header.Get("Retry-After"); value != "" {
		if seconds, err := time.ParseDuration(value); err == nil {
			apiErr.RetryAfter = max(apiErr.RetryAfter, seconds)
		} else if seconds, err := time.ParseDuration(value + "s"); err == nil {
			apiErr.RetryAfter = max(apiErr.RetryAfter, seconds)
		} else if retryAt, err := http.ParseTime(value); err == nil {
			apiErr.RetryAfter = max(apiErr.RetryAfter, time.Until(retryAt))
		}
	}
	classificationText = strings.NewReplacer("_", " ", "-", " ").Replace(classificationText)
	if strings.Contains(classificationText, "spend cap") || strings.Contains(classificationText, "spending cap") || strings.Contains(classificationText, "spending limit") || strings.Contains(classificationText, "billing limit") || strings.Contains(classificationText, "budget exceeded") {
		apiErr.Kind, apiErr.Code = GeminiFailureSpendCap, geminiFailureSpendCap
		return apiErr
	}
	switch statusCode {
	case http.StatusTooManyRequests:
		apiErr.Kind, apiErr.Code = GeminiFailureQuota, "rate_limited"
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		apiErr.Kind, apiErr.Code = GeminiFailureTemporary, "provider_unavailable"
	default:
		apiErr.Kind = GeminiFailureRejected
		if statusCode >= 500 {
			apiErr.Kind, apiErr.Code = GeminiFailureTemporary, "provider_error"
		}
	}
	return apiErr
}

type geminiResponseCapture struct {
	statusCode int
	header     http.Header
}

type geminiResponseTransport struct {
	base    http.RoundTripper
	capture *geminiResponseCapture
}

func (t geminiResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if response != nil {
		t.capture.statusCode = response.StatusCode
		t.capture.header = response.Header.Clone()
	}
	return response, err
}

func geminiHTTPClient(client *http.Client, timeout time.Duration, capture *geminiResponseCapture) *http.Client {
	result := http.Client{Timeout: timeout}
	if client != nil {
		result = *client
		if result.Timeout == 0 {
			result.Timeout = timeout
		}
	}
	transport := result.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	result.Transport = geminiResponseTransport{base: transport, capture: capture}
	return &result
}

func geminiBaseURL(endpoint string) (string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("invalid Gemini endpoint")
	}
	path := strings.TrimRight(parsed.Path, "/")
	path = strings.TrimSuffix(path, "/v1beta")
	parsed.Path = path
	parsed.RawPath = ""
	return parsed.String(), nil
}
