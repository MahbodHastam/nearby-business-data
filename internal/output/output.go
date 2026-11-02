package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

// Format picks the output format from the explicit value, falling back to
// the file extension and then to json.
func Format(format, path string) (string, error) {
	if format == "" {
		format = strings.TrimPrefix(filepath.Ext(path), ".")
	}
	switch format = strings.ToLower(format); format {
	case "", "json":
		return "json", nil
	case "csv":
		return "csv", nil
	}
	return "", fmt.Errorf("unsupported format %q, use json or csv", format)
}

func Write(w io.Writer, format string, bs []business.Business) error {
	if format == "csv" {
		return WriteCSV(w, bs)
	}
	return WriteJSON(w, bs)
}

func WriteJSON(w io.Writer, bs []business.Business) error {
	if bs == nil {
		bs = []business.Business{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(bs)
}

func WriteCSV(w io.Writer, bs []business.Business) error {
	cw := csv.NewWriter(w)
	cw.Write(business.CSVHeader)
	for _, b := range bs {
		cw.Write(b.CSVRow())
	}
	cw.Flush()
	return cw.Error()
}
