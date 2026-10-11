package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"uuid"
)

var ErrUnauthenticated = errors.New("unauthenticated")

const sessionCookie = "horse_session"
const oidcStateCookie = "horse_oidc_state"

type Config struct {
	IssuerURL      string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	PublicOrigin   string
	FrontendOrigin string
}

type Principal struct {
	MemberID    string `json:"id"`
	Subject     string `json:"subject"`
	DisplayName string `json:"displayName"`
}

type Service struct {
	db       *pgxpool.Pool
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	oauth    oauth2.Config
	cfg      Config
}

type claims struct {
	Subject           string `json:"sub"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
}

func New(ctx context.Context, db *pgxpool.Pool, cfg Config) (*Service, error) {
	if cfg.IssuerURL == "" || cfg.ClientID == "" || cfg.RedirectURL == "" {
		return nil, nil
	}
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("discover OIDC provider: %w", err)
	}
	verifier := provider.Verifier(&oidc.Config{ClientID: cfg.ClientID})
	return &Service{
		db: db, provider: provider, verifier: verifier,
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			Endpoint: provider.Endpoint(), RedirectURL: cfg.RedirectURL,
			Scopes: []string{oidc.ScopeOpenID, "profile", "email"},
		},
		cfg: cfg,
	}, nil
}

func (s *Service) Enabled() bool { return s != nil }

func (s *Service) Login(w http.ResponseWriter, r *http.Request) {
	verifier := oauth2.GenerateVerifier()
	state, err := randomToken(32)
	if err != nil {
		http.Error(w, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	nonce, err := randomToken(32)
	if err != nil {
		http.Error(w, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	redirectTo := safeRedirect(r.URL.Query().Get("returnTo"))
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	_, err = s.db.Exec(ctx, `INSERT INTO oidc_states(state_hash, nonce, verifier, redirect_to, expires_at) VALUES ($1, $2, $3, $4, now() + interval '10 minutes')`, digest(state), nonce, verifier, redirectTo)
	if err != nil {
		http.Error(w, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	// #nosec G124 -- HTTPSのOriginではSecureを有効にし、ローカルHTTP開発だけ無効にする。
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: state, Path: "/auth/callback", MaxAge: 600, HttpOnly: true, Secure: s.secureCookie(), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)), http.StatusFound)
}

func (s *Service) Callback(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if state == "" || code == "" {
		http.Error(w, "invalid authentication response", http.StatusBadRequest)
		return
	}
	stateCookie, cookieErr := r.Cookie(oidcStateCookie)
	// #nosec G124 -- HTTPSのOriginではSecureを有効にし、ローカルHTTP開発だけ無効にする。
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/auth/callback", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie(), SameSite: http.SameSiteLaxMode})
	if cookieErr != nil || subtle.ConstantTimeCompare([]byte(stateCookie.Value), []byte(state)) != 1 {
		http.Error(w, "invalid authentication state", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var nonce, verifier, redirectTo string
	err := s.db.QueryRow(ctx, `DELETE FROM oidc_states WHERE state_hash = $1 AND expires_at > now() RETURNING nonce, verifier, redirect_to`, digest(state)).Scan(&nonce, &verifier, &redirectTo)
	if err != nil {
		http.Error(w, "invalid or expired authentication state", http.StatusBadRequest)
		return
	}
	token, err := s.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	var identity claims
	if err := idToken.Claims(&identity); err != nil || identity.Subject == "" || idToken.Nonce != nonce {
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}
	displayName := identity.Name
	if displayName == "" {
		displayName = identity.PreferredUsername
	}
	if displayName == "" {
		displayName = identity.Email
	}
	if displayName == "" {
		displayName = identity.Subject
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO members(id, oidc_subject, display_name) VALUES ($1, $2, $3) ON CONFLICT (oidc_subject) DO UPDATE SET display_name = EXCLUDED.display_name`, uuid.NewV7(), identity.Subject, displayName); err != nil {
		http.Error(w, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	session, err := randomToken(32)
	if err != nil {
		http.Error(w, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	var memberID string
	if err := s.db.QueryRow(ctx, `SELECT id::text FROM members WHERE oidc_subject = $1`, identity.Subject).Scan(&memberID); err != nil {
		http.Error(w, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	expiry := time.Now().Add(24 * time.Hour)
	if !token.Expiry.IsZero() && token.Expiry.Before(expiry) {
		expiry = token.Expiry
	}
	if idToken.Expiry.Before(expiry) {
		expiry = idToken.Expiry
	}
	if !expiry.After(time.Now()) {
		http.Error(w, "authentication session has expired", http.StatusUnauthorized)
		return
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO oidc_sessions(token_hash, member_id, expiry) VALUES ($1, $2, $3)`, digest(session), memberID, expiry); err != nil {
		http.Error(w, "authentication unavailable", http.StatusInternalServerError)
		return
	}
	// #nosec G124 -- HTTPSのOriginではSecureを有効にし、ローカルHTTP開発だけ無効にする。
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: session, Path: "/", Expires: expiry,
		HttpOnly: true, Secure: s.secureCookie(), SameSite: http.SameSiteLaxMode,
	})
	http.Redirect(w, r, redirectTo, http.StatusSeeOther)
}

func (s *Service) Logout(w http.ResponseWriter, r *http.Request) {
	if err := sameOrigin([]string{s.cfg.PublicOrigin, s.cfg.FrontendOrigin}, r); err != nil {
		http.Error(w, "request origin rejected", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if _, err := s.db.Exec(ctx, `DELETE FROM oidc_sessions WHERE token_hash = $1`, digest(cookie.Value)); err != nil {
			http.Error(w, "authentication unavailable", http.StatusInternalServerError)
			return
		}
	}
	// #nosec G124 -- HTTPSのOriginではSecureを有効にし、ローカルHTTP開発だけ無効にする。
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie(), SameSite: http.SameSiteLaxMode})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) Resolve(r *http.Request) (Principal, error) {
	if s == nil {
		return Principal{}, ErrUnauthenticated
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return Principal{}, ErrUnauthenticated
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	var principal Principal
	err = s.db.QueryRow(ctx, `SELECT m.id::text, m.oidc_subject, m.display_name FROM oidc_sessions s JOIN members m ON m.id = s.member_id WHERE s.token_hash = $1 AND s.expiry > now()`, digest(cookie.Value)).Scan(&principal.MemberID, &principal.Subject, &principal.DisplayName)
	if err != nil {
		return Principal{}, ErrUnauthenticated
	}
	return principal, nil
}

func (s *Service) Me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	principal, err := s.Resolve(r)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"authenticated":false,"loginURL":"/auth/login"}`))
		return
	}
	if err := json.MarshalWrite(w, map[string]any{"authenticated": true, "viewer": principal}); err != nil {
		http.Error(w, "response encoding failed", http.StatusInternalServerError)
	}
}

func (s *Service) secureCookie() bool {
	parsed, err := url.Parse(s.cfg.PublicOrigin)
	return err == nil && parsed.Scheme == "https"
}

func safeRedirect(value string) string {
	if value == "" {
		return "/"
	}
	for _, char := range value {
		if unicode.IsControl(char) {
			return "/"
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || !strings.HasPrefix(parsed.Path, "/") || strings.HasPrefix(parsed.Path, "//") || strings.Contains(parsed.Path, "\\") || strings.ContainsAny(value, "\r\n") {
		return "/"
	}
	return parsed.RequestURI()
}

func sameOrigin(expected []string, r *http.Request) error {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return errors.New("origin header is required")
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("invalid origin")
	}
	for _, value := range expected {
		expectedURL, err := url.Parse(value)
		if err == nil && expectedURL.Host != "" && strings.EqualFold(parsed.Scheme, expectedURL.Scheme) && strings.EqualFold(parsed.Host, expectedURL.Host) {
			return nil
		}
	}
	return errors.New("origin mismatch")
}

func randomToken(size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func digest(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}
