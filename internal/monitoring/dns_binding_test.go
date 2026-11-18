package monitoring

import "testing"

func TestDNSBindingIncludesFullRecordSet(t *testing.T) {
	r := []string{"HOST|monitor.owned-test.example.com", "monitor.owned-test.example.com|CNAME|edge.owned-test.example.com", "edge.owned-test.example.com|A|93.184.216.34", "edge.owned-test.example.com|AAAA|2001:4860:ffff::34"}
	h := DNSBinding(r)
	if h != DNSBinding([]string{r[3], r[0], r[2], r[1], r[1]}) {
		t.Fatal("RR order or duplicates changed identity")
	}
	for i := range r {
		c := append([]string(nil), r...)
		c[i] += "x"
		if h == DNSBinding(c) {
			t.Fatal("unbound RR", i)
		}
	}
	if h == DNSBinding(r[:3]) {
		t.Fatal("AAAA omission")
	}
}
