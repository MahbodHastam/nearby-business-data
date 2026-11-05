package output

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mahbodhastam/nearby-business-data/internal/business"
)

var sample = []business.Business{{
	Name:    "Milja & Schäfa",
	Rating:  4.7,
	Reviews: 1878,
	Address: "Sonntagstraße 1, 10245 Berlin",
	Hours:   []string{"Monday: 8 am–9 pm", "Tuesday: 8 am–9 pm"},
	Lat:     52.50457,
	Lng:     13.468107,
	URL:     "https://www.google.com/maps/place/x",
}}

func TestFormat(t *testing.T) {
	tests := []struct {
		flag, path string
		want       string
		wantErr    bool
	}{
		{"", "", "json", false},
		{"", "out.csv", "csv", false},
		{"", "out.JSON", "json", false},
		{"csv", "out.json", "csv", false},
		{"xml", "", "", true},
		{"", "out.txt", "", true},
	}
	for _, tt := range tests {
		got, err := Format(tt.flag, tt.path)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("Format(%q, %q) = %q, %v; want %q, err=%v", tt.flag, tt.path, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, sample); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Milja & Schäfa") {
		t.Errorf("want unescaped name, got:\n%s", buf.String())
	}
	var got []business.Business
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Name != sample[0].Name || len(got[0].Hours) != 2 {
		t.Errorf("round trip = %+v", got)
	}

	buf.Reset()
	WriteJSON(&buf, nil)
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("nil slice should encode as [], got %q", buf.String())
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, sample); err != nil {
		t.Fatal(err)
	}
	want := "name,category,rating,reviews,address,phone,website,hours,open_now,lat,lng,distance_m,place_id,url\n" +
		`Milja & Schäfa,,4.7,1878,"Sonntagstraße 1, 10245 Berlin",,,Monday: 8 am–9 pm; Tuesday: 8 am–9 pm,,52.50457,13.468107,,,https://www.google.com/maps/place/x` + "\n"
	if buf.String() != want {
		t.Errorf("CSV mismatch\ngot:\n%s\nwant:\n%s", buf.String(), want)
	}
}

func TestWriteCSVOpenNowAndDistance(t *testing.T) {
	open, closed := true, false
	var buf bytes.Buffer
	WriteCSV(&buf, []business.Business{
		{Name: "A", OpenNow: &open, DistanceM: 120, URL: "u"},
		{Name: "B", OpenNow: &closed, URL: "u"},
	})
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if lines[1] != "A,,0,0,,,,,true,0,0,120,,u" || lines[2] != "B,,0,0,,,,,false,0,0,,,u" {
		t.Errorf("rows:\n%s", strings.Join(lines[1:], "\n"))
	}
}
