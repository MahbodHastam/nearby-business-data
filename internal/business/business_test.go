package business

import "testing"

func TestParsePoint(t *testing.T) {
	p, err := ParsePoint(" 52.52 , -13.405")
	if err != nil || p.Lat != 52.52 || p.Lng != -13.405 {
		t.Errorf("got %+v, %v", p, err)
	}
	for _, bad := range []string{"", "52.52", "a,b", "91,0", "0,181", "1,2,3"} {
		if _, err := ParsePoint(bad); err == nil {
			t.Errorf("ParsePoint(%q): want error", bad)
		}
	}
}
