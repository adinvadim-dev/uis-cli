package portal

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
)

// Import only parses/validates the file into editor rows; the campaign is not
// saved. The file is sent once using the frontend's file + old_numbers fields.
func autodialImportBody(payload Object) (*bytes.Buffer, string, error) {
	path, ok := payload["input_file"].(string)
	if !ok || path == "" {
		return nil, "", &Error{Code: 2, Stage: "autodial_import_file"}
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, "", &Error{Code: 2, Stage: "autodial_import_file"}
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, (10<<20)+1))
	if e != nil || len(raw) > 10<<20 {
		return nil, "", &Error{Code: 2, Stage: "autodial_import_size"}
	}
	old := payload["old_numbers"]
	if old == nil {
		old = []interface{}{}
	}
	switch old.(type) {
	case []interface{}, []Object:
	default:
		return nil, "", &Error{Code: 2, Stage: "autodial_import_old_numbers"}
	}
	oldJSON, e := json.Marshal(old)
	if e != nil {
		return nil, "", &Error{Code: 2, Stage: "autodial_import_old_numbers"}
	}
	b := &bytes.Buffer{}
	w := multipart.NewWriter(b)
	if e = w.WriteField("old_numbers", string(oldJSON)); e != nil {
		return nil, "", e
	}
	part, e := w.CreateFormFile("file", filepath.Base(path))
	if e != nil {
		return nil, "", e
	}
	if _, e = part.Write(raw); e != nil {
		return nil, "", e
	}
	if e = w.Close(); e != nil {
		return nil, "", e
	}
	return b, w.FormDataContentType(), nil
}
