package porthop

import (
	"reflect"
	"testing"
)

func TestParse(t *testing.T) {
	got, err := Parse(" 300-400,100,101-200,150-299,65535 ")
	if err != nil || !reflect.DeepEqual(got, []Range{{100, 400}, {65535, 65535}}) {
		t.Fatalf("got %v, %v", got, err)
	}
	for _, raw := range []string{"", "0", "65536", "-1", "+1", "3-2", "1-2-3", "1,", "1,,2", "1; flush ruleset", "1\n2", "1:2"} {
		t.Run(raw, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Fatalf("accepted %q", raw)
			}
		})
	}
}

func TestFromStream(t *testing.T) {
	for _, raw := range []string{
		`{"finalmask":{"quicParams":{"udpHop":{"ports":"20000-30000","interval":5}}}}`,
		`{"finalmask":{"udp":[{"type":"udphop","settings":{"remotePorts":"20000-30000"}}],"quicParams":{"udpHop":{"ports":"40000"}}}}`,
	} {
		got, err := FromStream(raw)
		if err != nil || !reflect.DeepEqual(got, []Range{{20000, 30000}}) {
			t.Fatalf("got %v, %v", got, err)
		}
	}
	for _, raw := range []string{"", `{}`, `{"finalmask":{"udp":[{"type":"salamander"}]}}`} {
		got, err := FromStream(raw)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %v, %v", got, err)
		}
	}
	for _, raw := range []string{`{`, `{"finalmask":{"quicParams":{"udpHop":{"ports":""}}}}`, `{"finalmask":{"udp":[{"type":"udphop","settings":{"remotePorts":123}}]}}`} {
		if _, err := FromStream(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
