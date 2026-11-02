package proxy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	p, err := Load("  ")
	if err != nil || p != nil {
		t.Fatalf("Load(empty) = %v, %v; want nil, nil", p, err)
	}
	if p.Next() != nil || p.Len() != 0 {
		t.Error("nil pool should yield no proxies")
	}
}

func TestLoadListRoundRobin(t *testing.T) {
	p, err := Load("http://a:1, socks5://b:2")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for range 4 {
		got = append(got, p.Next().Host)
	}
	want := []string{"a:1", "b:2", "a:1", "b:2"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Next sequence = %v; want %v", got, want)
		}
	}
}

func TestLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxies.txt")
	data := "# my proxies\nhttp://user:pass@a:8080\n\nhttp://b:8080 # backup\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	p, err := Load("@" + path)
	if err != nil {
		t.Fatal(err)
	}
	if p.Len() != 2 {
		t.Fatalf("Len = %d; want 2", p.Len())
	}
	if u := p.Next(); u.User.Username() != "user" {
		t.Errorf("credentials lost: %v", u)
	}
}

func TestLoadInvalid(t *testing.T) {
	if _, err := Load("not a url"); err == nil {
		t.Error("want error for proxy without host")
	}
	if _, err := Load("@/does/not/exist"); err == nil {
		t.Error("want error for missing file")
	}
}
