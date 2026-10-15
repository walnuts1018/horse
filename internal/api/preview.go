package api

import (
	"bytes"
	"encoding/json/v2"
	"errors"
	"image"
	_ "image/png"
	"net/http"
	"strconv"

	gofrsuuid "github.com/gofrs/uuid/v5"
	"github.com/jackc/pgx/v5"
	bobpgx "github.com/stephenafamo/bob/drivers/pgx"
	"github.com/walnuts1018/horse/internal/ingestion"
)

const maxPreviewPixels = 40_000_000

func (s *Server) documentPreview(w http.ResponseWriter, r *http.Request) {
	principal, err := s.authenticate(r)
	if err != nil {
		s.writeAuthenticationFailure(w, r, err)
		return
	}
	if s.cfg.Store == nil {
		s.writeOperationalError(w, r, "document storage is unavailable", http.StatusServiceUnavailable, errors.New("document store is not configured"))
		return
	}
	if s.cfg.OCR.Endpoint == "" {
		s.writeOperationalError(w, r, "document preview is unavailable", http.StatusServiceUnavailable, errors.New("OCR endpoint is not configured"))
		return
	}
	page := 0
	if values, ok := r.URL.Query()["page"]; ok {
		if len(values) != 1 {
			http.Error(w, "invalid page", http.StatusBadRequest)
			return
		}
		page, err = strconv.Atoi(values[0])
		if err != nil || page < 0 {
			http.Error(w, "invalid page", http.StatusBadRequest)
			return
		}
	}
	documentID, err := gofrsuuid.FromString(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	memberID, err := gofrsuuid.FromString(principal.MemberID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	document, err := documentForMember(r.Context(), bobpgx.NewPool(s.db), documentID, memberID, false)
	if errors.Is(err, pgx.ErrNoRows) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.writeOperationalError(w, r, "document preview lookup failed", http.StatusInternalServerError, err)
		return
	}
	if !supportedPreviewType(document.MimeType) || (document.MimeType != documentMIMEPDF && page != 0) {
		http.Error(w, "invalid page or document type", http.StatusBadRequest)
		return
	}
	original, err := s.cfg.Store.Get(r.Context(), document.ObjectKey, document.MimeType)
	if err != nil {
		s.writeOperationalError(w, r, "document preview source unavailable", http.StatusBadGateway, err)
		return
	}
	if len(original.Data) == 0 || len(original.Data) > ingestion.MaxDocumentBytes {
		s.writeOperationalError(w, r, "document preview source invalid", http.StatusBadGateway, errors.New("stored document size is invalid"))
		return
	}
	preview, transform, err := s.cfg.OCR.Crop(r.Context(), original, ingestion.Region{Page: page, Top: 0, Left: 0, Bottom: 1, Right: 1})
	if errors.Is(err, ingestion.ErrInvalidCropPage) {
		http.Error(w, "invalid page", http.StatusBadRequest)
		return
	}
	if err != nil {
		s.writeOperationalError(w, r, "document preview generation failed", http.StatusBadGateway, err)
		return
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(preview.Data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > maxPreviewPixels {
		if err == nil {
			err = errors.New("preview image dimensions are invalid")
		}
		s.writeOperationalError(w, r, "document preview image invalid", http.StatusBadGateway, err)
		return
	}
	transformJSON, err := json.Marshal(transform)
	if err != nil {
		s.writeOperationalError(w, r, "document preview encoding failed", http.StatusBadGateway, err)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", "inline")
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Original-Content-Type", document.MimeType)
	w.Header().Set("X-Source-Page", strconv.Itoa(page))
	w.Header().Set("X-Coordinate-Transform", string(transformJSON))
	w.Header().Set("Content-Length", strconv.Itoa(len(preview.Data)))
	if _, err := w.Write(preview.Data); err != nil {
		s.logger.WarnContext(r.Context(), "document preview response write failed", "document_id", documentID, "error", err)
	}
}

func supportedPreviewType(mimeType string) bool {
	switch mimeType {
	case documentMIMEJPEG, documentMIMEPNG, documentMIMEWebP, documentMIMEPDF:
		return true
	default:
		return false
	}
}
