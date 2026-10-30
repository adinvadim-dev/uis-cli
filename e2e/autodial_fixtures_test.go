//go:build production

package e2e

import (
	"archive/zip"
	"os"
	"testing"
)

// A minimal Office Open XML fixture: inline strings preserve phone digits.
// It is test input to the provider parser, never a campaign contact upload/save.
func autodialXLSX(t *testing.T, path string) {
	t.Helper()
	f, e := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal("cannot stage XLSX")
	}
	z := zip.NewWriter(f)
	parts := []struct{ name, body string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Contacts" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{"xl/_rels/workbook.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`},
		{"xl/worksheets/sheet1.xml", `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>numa</t></is></c><c r="B1" t="inlineStr"><is><t>name</t></is></c><c r="C1" t="inlineStr"><is><t>comment</t></is></c></row><row r="2"><c r="A2" t="inlineStr"><is><t>70000000000</t></is></c><c r="B2" t="inlineStr"><is><t>uis-cli e2e import</t></is></c><c r="C2" t="inlineStr"><is><t>do not call</t></is></c></row></sheetData></worksheet>`},
	}
	for _, p := range parts {
		w, e := z.Create(p.name)
		if e != nil {
			t.Fatal("cannot stage XLSX part")
		}
		if _, e = w.Write([]byte(p.body)); e != nil {
			t.Fatal("cannot write XLSX part")
		}
	}
	if z.Close() != nil || f.Close() != nil {
		t.Fatal("cannot finish XLSX")
	}
}
