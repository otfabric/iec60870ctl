// SPDX-License-Identifier: MIT

package format

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

type row struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
	cols  int
}

func (r row) MarshalTextOutput() (string, error) {
	if r.Name == "broken" {
		return "", errors.New("cannot render")
	}
	return r.Name + "\n", nil
}
func (r row) TableHeaders() []string {
	if r.cols < 0 {
		return nil
	}
	return []string{"NAME", "VALUE"}
}
func (r row) TableRows() [][]string {
	if r.cols > 0 {
		return [][]string{{r.Name}}
	}
	return [][]string{{r.Name, strings.Repeat("9", r.Value)}}
}

type failing struct{}

func (failing) Write([]byte) (int, error) { return 0, errors.New("disk full") }

func TestParse(t *testing.T) {
	for in, want := range map[string]OutputFormat{"": FormatText, " JSON ": FormatJSON, "table": FormatTable, "Text": FormatText} {
		if got, err := Parse(in); err != nil || got != want {
			t.Errorf("Parse(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := Parse("xml"); err == nil || !strings.Contains(err.Error(), "text, json, table") {
		t.Errorf("Parse(xml): %v", err)
	}
	d := ValueDescriptions()
	d["json"] = "changed"
	if len(d) != 3 || ValueDescriptions()["json"] == "changed" {
		t.Error("ValueDescriptions must return a copy with the three formats")
	}
}

func TestWrite(t *testing.T) {
	var b bytes.Buffer
	if err := Write(&b, FormatText, row{Name: "a"}); err != nil || b.String() != "a\n" {
		t.Errorf("text: %q, %v", b.String(), err)
	}
	b.Reset()
	if err := Write(&b, FormatJSON, row{Name: "a", Value: 2}); err != nil || b.String() != "{\n  \"name\": \"a\",\n  \"value\": 2\n}\n" {
		t.Errorf("json: %q, %v", b.String(), err)
	}
	b.Reset()
	if err := Write(&b, FormatTable, row{Name: "abc", Value: 1}); err != nil || b.String() != "NAME  VALUE\n-     -\nabc   9\n" {
		t.Errorf("table: %q, %v", b.String(), err)
	}
	for name, err := range map[string]error{
		"text of a value without text":   Write(&b, FormatText, 1),
		"table of a value without table": Write(&b, FormatTable, 1),
		"text that fails":                Write(&b, FormatText, row{Name: "broken"}),
		"table without headers":          Write(&b, FormatTable, row{cols: -1}),
		"ragged table":                   Write(&b, FormatTable, row{cols: 1}),
		"json of a channel":              Write(&b, FormatJSON, make(chan int)),
		"unknown format":                 Write(&b, "xml", row{}),
		"writer fails (text)":            Write(failing{}, FormatText, row{Name: "a"}),
		"writer fails (json)":            Write(failing{}, FormatJSON, row{Name: "a"}),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestStream(t *testing.T) {
	var b bytes.Buffer
	s := NewStreamWriter(&b, FormatJSON)
	_ = s.Write(row{Name: "a", Value: 1})
	_ = s.Write(row{Name: "b", Value: 2})
	if b.String() != "{\"name\":\"a\",\"value\":1}\n{\"name\":\"b\",\"value\":2}\n" {
		t.Errorf("json lines: %q", b.String())
	}
	b.Reset()
	s = NewStreamWriter(&b, FormatText)
	_ = s.Write(row{Name: "a"})
	_ = s.Write(row{Name: "b"})
	if b.String() != "a\nb\n" {
		t.Errorf("text stream: %q", b.String())
	}
	// A table stream writes its header once; the widths of the first record stay.
	b.Reset()
	s = NewStreamWriter(&b, FormatTable)
	_ = s.Write(row{Name: "first", Value: 10})
	_ = s.Write(row{Name: "a-much-longer-name", Value: 1})
	want := "NAME      VALUE\n-         -\nfirst     9999999999\na-much-longer-name  9\n"
	if b.String() != want {
		t.Errorf("table stream:\n%q\nwant\n%q", b.String(), want)
	}
	for name, err := range map[string]error{
		"text":          NewStreamWriter(&b, FormatText).Write(1),
		"text fails":    NewStreamWriter(&b, FormatText).Write(row{Name: "broken"}),
		"table":         NewStreamWriter(&b, FormatTable).Write(1),
		"no headers":    NewStreamWriter(&b, FormatTable).Write(row{cols: -1}),
		"ragged":        NewStreamWriter(&b, FormatTable).Write(row{cols: 1}),
		"json":          NewStreamWriter(&b, FormatJSON).Write(make(chan int)),
		"unknown":       NewStreamWriter(&b, "xml").Write(row{}),
		"writer (json)": NewStreamWriter(failing{}, FormatJSON).Write(row{}),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
