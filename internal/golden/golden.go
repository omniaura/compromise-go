// Package golden reads the parity goldens written by tools/dump-model/goldens.mjs.
package golden

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
)

// Term is [text, pre, post, normal, implicit, machine, offset.start, tags].
type Term struct {
	Text, Pre, Post, Normal, Implicit, Machine string
	Start                                      int
	Tags                                       []string
}

func (t *Term) UnmarshalJSON(b []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if len(raw) != 8 {
		return fmt.Errorf("golden term has %d fields", len(raw))
	}
	strs := []*string{&t.Text, &t.Pre, &t.Post, &t.Normal, &t.Implicit, &t.Machine}
	for i, p := range strs {
		if err := json.Unmarshal(raw[i], p); err != nil {
			return err
		}
	}
	if err := json.Unmarshal(raw[6], &t.Start); err != nil {
		return err
	}
	return json.Unmarshal(raw[7], &t.Tags)
}

// Row is one corpus document and what JS compromise made of it.
type Row struct {
	Text      string   `json:"text"`
	Sentences [][]Term `json:"sentences"`
	Values    []string `json:"values"`
	ToNumber  []string `json:"toNumber"`
	LLM       [][2]int `json:"llm"`
	NThings   [][2]int `json:"nthings"`
	// NThingsFixed is the corrected no-n-things (see goldens.mjs).
	NThingsFixed [][2]int `json:"nthingsFixed"`
}

// Load reads a gzipped JSONL golden file.
func Load(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	var rows []Row
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var r Row
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, err
		}
		rows = append(rows, r)
	}
	return rows, sc.Err()
}
