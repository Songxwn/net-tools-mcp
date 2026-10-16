package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSplitASOrg(t *testing.T) {
	asn, name := splitASOrg("AS15169 Google LLC")
	if asn != "15169" || name != "Google LLC" {
		t.Fatalf("%s %s", asn, name)
	}
	if got := normalizeASN("AS38136"); got != "38136" {
		t.Fatal(got)
	}
}

func TestFallbackParsers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Header.Get("X-Orig-Host")
		switch host {
		case "api.ip.sb":
			w.Write([]byte(`{"region":"California","organization":"Google","isp":"Google","city":"Mountain View","asn":15169,"latitude":37.751,"ip":"8.8.8.8","country":"United States","country_code":"US","longitude":-97.822}`))
		case "ipinfo.io":
			w.Write([]byte(`{"ip":"8.8.8.8","city":"Mountain View","country":"US","loc":"38.0088,-122.1175","org":"AS15169 Google LLC","postal":"94043"}`))
		case "ipwho.is":
			w.Write([]byte(`{"ip":"8.8.8.8","success":true,"country":"United States","country_code":"US","city":"San Jose","latitude":37.33,"longitude":-121.89,"postal":"95025","connection":{"asn":15169,"org":"Google LLC","isp":"Google LLC"}}`))
		case "ifconfig.co":
			w.Write([]byte(`{"ip":"8.8.8.8","country":"United States","country_iso":"US","latitude":37.751,"longitude":-97.822,"asn":"AS15169","asn_org":"GOOGLE"}`))
		case "ident.me":
			w.Write([]byte(`{"ip":"188.253.127.89","aso":"Akari Networks Limited","asn":38136,"country":"Hong Kong","cc":"HK","city":"Hong Kong","latitude":22.37,"longitude":114.12}`))
		case "ip-api.com":
			w.Write([]byte(`{"status":"success","country":"United States","countryCode":"US","city":"Ashburn","zip":"20149","lat":39.03,"lon":-77.5,"isp":"Google LLC","org":"Google Public DNS","as":"AS15169 Google LLC","query":"8.8.8.8"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := srv.Client()
	client.Transport = hostRewrite{base: client.Transport, dest: strings.TrimPrefix(srv.URL, "http://")}

	ctx := context.Background()
	sb, err := lookupIPSB(ctx, client, "8.8.8.8")
	if err != nil || sb.ASN != "15169" || sb.City != "Mountain View" || sb.Latitude != "37.751" {
		t.Fatalf("ipsb %+v %v", sb, err)
	}
	info, err := lookupIPInfo(ctx, client, "8.8.8.8")
	if err != nil || info.ASN != "15169" || info.Organization != "Google LLC" || info.CountryCode != "US" || info.Longitude != "-122.1175" {
		t.Fatalf("ipinfo %+v %v", info, err)
	}
	who, err := lookupIPWho(ctx, client, "8.8.8.8")
	if err != nil || who.ISP != "Google LLC" || who.ASN != "15169" {
		t.Fatalf("ipwho %+v %v", who, err)
	}
	ifc, err := lookupIfconfigCo(ctx, client, "8.8.8.8")
	if err != nil || ifc.ASN != "15169" || ifc.Organization != "GOOGLE" {
		t.Fatalf("ifconfig %+v %v", ifc, err)
	}
	ident, err := lookupIdentMe(ctx, client, "")
	if err != nil || ident.CountryCode != "HK" || ident.ASN != "38136" {
		t.Fatalf("ident %+v %v", ident, err)
	}
	if _, err := lookupIdentMe(ctx, client, "8.8.8.8"); err == nil {
		t.Fatal("ident.me accepted a specific ip")
	}
	api, err := lookupIPAPI(ctx, client, "8.8.8.8")
	if err != nil || api.IP != "8.8.8.8" || api.Organization != "Google Public DNS" || api.ASN != "15169" {
		t.Fatalf("ip-api %+v %v", api, err)
	}
}

type hostRewrite struct {
	base http.RoundTripper
	dest string
}

func (h hostRewrite) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header.Set("X-Orig-Host", req.URL.Hostname())
	clone.URL.Scheme = "http"
	clone.URL.Host = h.dest
	clone.Host = h.dest
	base := h.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}
