package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const sampleIPMeHTML = `
<p class="ip-address">188.253.127.89</p>
<table>
  <tr><th>City:</th><td><code>Hong Kong</code></td></tr>
  <tr><th>Country:</th><td><code>Hong Kong</code></td></tr>
  <tr><th>Country Code:</th><td><code>HK</code></td></tr>
  <tr><th>Latitude:</th><td class="latitude"><code>22.2842</code></td></tr>
  <tr><th>Longitude:</th><td class="longitude"><code>114.1759</code></td></tr>
  <tr><th>Postal Code:</th><td><code>None</code></td></tr>
  <tr><th>Organization:</th><td><code>Akari Networks</code></td></tr>
  <tr><th>ASN:</th><td><code>38136</code></td></tr>
  <tr><th>ISP Name:</th><td><code>Akari Networks</code></td></tr>
</table>
`

func TestParsePlainIP(t *testing.T) {
	ip, err := parsePlainIP("188.253.127.89\n")
	if err != nil || ip != "188.253.127.89" {
		t.Fatalf("%s %v", ip, err)
	}
	if _, err := parsePlainIP("<html>nope</html>"); err == nil {
		t.Fatal("html accepted as ip")
	}
}

func TestParseIPMeHTML(t *testing.T) {
	got := parseIPMeHTML(sampleIPMeHTML)
	if got.IP != "188.253.127.89" || got.City != "Hong Kong" || got.CountryCode != "HK" {
		t.Fatalf("%+v", got)
	}
	if got.PostalCode != "" {
		t.Fatalf("none should be empty: %+v", got)
	}
	if got.Organization != "Akari Networks" || got.ASN != "38136" || got.ISP != "Akari Networks" {
		t.Fatalf("%+v", got)
	}
	if got.Latitude != "22.2842" || got.Longitude != "114.1759" {
		t.Fatalf("%+v", got)
	}
}

func TestFormatIPMe(t *testing.T) {
	text := formatIPMe(ipMeOutput{IP: "8.8.8.8", Country: "United States", Organization: "Google", ASN: "15169", ISP: "Google", Source: "https://api.ip.sb/geoip"})
	if text != "公网 IP: 8.8.8.8，归属: United States，组织: Google，ASN: 15169，来源: https://api.ip.sb/geoip" {
		t.Fatalf("%s", text)
	}
}

func TestLookupSourcesFallsBack(t *testing.T) {
	sources := []geoSource{
		{name: "ip.me", lookup: func(context.Context, *http.Client, string) (ipMeOutput, error) {
			return ipMeOutput{}, fmt.Errorf("HTTP 429")
		}},
		{name: "ip.sb", lookup: func(context.Context, *http.Client, string) (ipMeOutput, error) {
			return ipMeOutput{}, fmt.Errorf("无归属信息")
		}},
		{name: "ipinfo.io", lookup: func(context.Context, *http.Client, string) (ipMeOutput, error) {
			return ipMeOutput{IP: "8.8.8.8", Country: "United States", ASN: "15169", Source: "https://ipinfo.io/json"}, nil
		}},
		{name: "later", lookup: func(context.Context, *http.Client, string) (ipMeOutput, error) {
			return ipMeOutput{IP: "1.1.1.1", Country: "Australia", Source: "later"}, nil
		}},
	}
	got, err := lookupSources(context.Background(), nil, "", sources, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "https://ipinfo.io/json" || got.ASN != "15169" {
		t.Fatalf("%+v", got)
	}
}

func TestLookupSourcesPrefersEarlierAttribution(t *testing.T) {
	sources := []geoSource{
		{name: "ip.me", lookup: func(ctx context.Context, _ *http.Client, _ string) (ipMeOutput, error) {
			<-ctx.Done()
			return ipMeOutput{}, ctx.Err()
		}},
		{name: "ip.sb", lookup: func(context.Context, *http.Client, string) (ipMeOutput, error) {
			return ipMeOutput{IP: "8.8.8.8", Country: "United States", Source: "https://api.ip.sb/geoip"}, nil
		}},
	}
	start := time.Now()
	got, err := lookupSources(context.Background(), nil, "", sources, 200*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "https://api.ip.sb/geoip" || time.Since(start) > time.Second {
		t.Fatalf("%+v elapsed %s", got, time.Since(start))
	}
}

func TestLookupSourcesKeepsIPWhenAttributionMissing(t *testing.T) {
	sources := []geoSource{
		{name: "ip.me", lookup: func(context.Context, *http.Client, string) (ipMeOutput, error) {
			return ipMeOutput{IP: "1.2.3.4", Source: "https://ip.me"}, nil
		}},
	}
	got, err := lookupSources(context.Background(), nil, "", sources, time.Second)
	if err != nil || got.IP != "1.2.3.4" {
		t.Fatalf("%+v %v", got, err)
	}
	if !strings.Contains(formatIPMe(got), "未获取到归属") {
		t.Fatal(formatIPMe(got))
	}
}

func TestDefaultGeoSources(t *testing.T) {
	if len(defaultGeoSources) < 6 {
		t.Fatalf("sources: %d", len(defaultGeoSources))
	}
	if defaultGeoSources[0].name != "ip.me" || defaultGeoSources[1].name != "ip.sb" {
		t.Fatalf("%s %s", defaultGeoSources[0].name, defaultGeoSources[1].name)
	}
}
