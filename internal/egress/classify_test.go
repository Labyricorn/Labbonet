package egress

import (
	"net"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		ip   string
		want string
	}{
		{"IPv4 loopback", "127.0.0.1", ClassLocal},
		{"IPv6 loopback", "::1", ClassLocal},
		{"RFC1918 10/8", "10.138.11.11", ClassLAN},
		{"RFC1918 192.168/16", "192.168.1.5", ClassLAN},
		{"RFC1918 172.16/12", "172.16.0.5", ClassLAN},
		{"link-local", "169.254.1.1", ClassLAN},
		{"IPv6 ULA", "fd12:3456:789a::1", ClassLAN},
		{"IPv6 link-local", "fe80::1", ClassLAN},
		{"public IPv4", "8.8.8.8", ClassExternal},
		{"public IPv6", "2001:4860:4860::8888", ClassExternal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ip := net.ParseIP(c.ip)
			if ip == nil {
				t.Fatalf("net.ParseIP(%q) returned nil", c.ip)
			}
			if got := Classify(ip); got != c.want {
				t.Fatalf("Classify(%s) = %q, want %q", c.ip, got, c.want)
			}
		})
	}
}

func TestIsMetadata(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"169.254.169.254", true},
		{"fd00:ec2::254", true},
		{"169.254.1.1", false},
		{"127.0.0.1", false},
		{"10.0.0.1", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("net.ParseIP(%q) returned nil", c.ip)
		}
		if got := IsMetadata(ip); got != c.want {
			t.Fatalf("IsMetadata(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}
