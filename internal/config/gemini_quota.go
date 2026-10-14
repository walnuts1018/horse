package config

import (
	"fmt"
	"strconv"
	"strings"
)

type GeminiQuotaKey struct {
	ProjectRoute string
	Model        string
}

type GeminiQuotaLimit struct {
	RequestsPerMinute int64
	InputTokensMinute int64
	RequestsPerDay    int64
}

func ParseGeminiQuotaLimits(raw string) (map[GeminiQuotaKey]GeminiQuotaLimit, error) {
	limits := make(map[GeminiQuotaKey]GeminiQuotaLimit)
	if strings.TrimSpace(raw) == "" {
		return limits, nil
	}
	for _, entry := range strings.Split(raw, ";") {
		selector, values, ok := strings.Cut(strings.TrimSpace(entry), "=")
		if !ok {
			return nil, fmt.Errorf("quota entry must use route/model=rpm/tpm/rpd")
		}
		route, model, ok := strings.Cut(selector, "/")
		if !ok || (route != "free" && route != "paid") || model == "" || strings.Contains(model, "/") {
			return nil, fmt.Errorf("quota entry has an invalid route or model")
		}
		parts := strings.Split(values, "/")
		if len(parts) != 3 {
			return nil, fmt.Errorf("quota entry must specify rpm, tpm, and rpd")
		}
		parsed := [3]int64{}
		for i, part := range parts {
			value, err := strconv.ParseInt(part, 10, 64)
			if err != nil || value <= 0 {
				return nil, fmt.Errorf("quota values must be positive integers")
			}
			parsed[i] = value
		}
		key := GeminiQuotaKey{ProjectRoute: route, Model: model}
		if _, exists := limits[key]; exists {
			return nil, fmt.Errorf("quota entry is duplicated")
		}
		limits[key] = GeminiQuotaLimit{
			RequestsPerMinute: parsed[0],
			InputTokensMinute: parsed[1],
			RequestsPerDay:    parsed[2],
		}
	}
	return limits, nil
}
