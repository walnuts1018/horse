package api

import (
	"encoding/json/v2"
	"errors"
	"io"
	"math"
	"net/http"

	"github.com/walnuts1018/horse/internal/ingestion"
)

func readAnalysisUpload(r *http.Request) ([]byte, string, *[9]float64, error) {
	file, header, err := r.FormFile("analysisFile")
	if errors.Is(err, http.ErrMissingFile) {
		if r.FormValue("analysisTransform") != "" {
			return nil, "", nil, errors.New("analysis transform requires an analysis file")
		}
		return nil, "", nil, nil
	}
	if err != nil {
		return nil, "", nil, err
	}
	defer func() { _ = file.Close() }()
	data, err := readDocumentPart(file)
	if err != nil {
		return nil, "", nil, err
	}
	var values []float64
	if err := json.Unmarshal([]byte(r.FormValue("analysisTransform")), &values); err != nil || len(values) != 9 {
		return nil, "", nil, errors.New("analysis transform must contain nine numbers")
	}
	var transform [9]float64
	copy(transform[:], values)
	if err := validateAnalysisTransform(transform); err != nil {
		return nil, "", nil, err
	}
	return data, header.Filename, &transform, nil
}

func readDocumentPart(file io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(file, ingestion.MaxDocumentBytes+1))
	if err != nil || len(data) == 0 || len(data) > ingestion.MaxDocumentBytes {
		return nil, errors.New("invalid document size")
	}
	return data, nil
}

func validateAnalysisTransform(transform [9]float64) error {
	for _, value := range transform {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("invalid analysis transform")
		}
	}
	if transform[6] != 0 || transform[7] != 0 || transform[8] != 1 {
		return errors.New("analysis transform must be affine")
	}
	determinant := transform[0]*transform[4] - transform[1]*transform[3]
	if math.Abs(determinant) < 1e-12 {
		return errors.New("analysis transform must be invertible")
	}
	for _, point := range [][2]float64{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		x := transform[0]*point[0] + transform[1]*point[1] + transform[2]
		y := transform[3]*point[0] + transform[4]*point[1] + transform[5]
		if x < 0 || x > 1 || y < 0 || y > 1 {
			return errors.New("analysis transform must map inside the original image")
		}
	}
	return nil
}
