package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

type geoSource struct {
	name   string
	lookup func(ctx context.Context, client *http.Client, query string) (ipMeOutput, error)
}

var defaultGeoSources = []geoSource{
	{name: "ip.me", lookup: lookupIPMeSite},
	{name: "ip.sb", lookup: lookupIPSB},
	{name: "ipinfo.io", lookup: lookupIPInfo},
	{name: "ipwho.is", lookup: lookupIPWho},
	{name: "ifconfig.co", lookup: lookupIfconfigCo},
	{name: "ident.me", lookup: lookupIdentMe},
	{name: "ip-api.com", lookup: lookupIPAPI},
}

type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(strings.TrimSpace(s))
		return nil
	}
	*f = flexString(string(b))
	return nil
}

func (f flexString) String() string { return cleanIPMeValue(string(f)) }

func lookupIPSB(ctx context.Context, client *http.Client, query string) (ipMeOutput, error) {
	rawURL := "https://api.ip.sb/geoip"
	if query != "" {
		rawURL += "/" + query
	}
	body, err := getText(ctx, client, rawURL, curlUA, "application/json")
	if err != nil {
		return ipMeOutput{}, err
	}
	var doc struct {
		IP           string     `json:"ip"`
		City         string     `json:"city"`
		Country      string     `json:"country"`
		CountryCode  string     `json:"country_code"`
		Latitude     flexString `json:"latitude"`
		Longitude    flexString `json:"longitude"`
		Organization string     `json:"organization"`
		ISP          string     `json:"isp"`
		ASN          flexString `json:"asn"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return ipMeOutput{}, fmt.Errorf("ip.sb 响应无法解析")
	}
	out := ipMeOutput{
		IP:           doc.IP,
		City:         cleanIPMeValue(doc.City),
		Country:      cleanIPMeValue(doc.Country),
		CountryCode:  cleanIPMeValue(doc.CountryCode),
		Latitude:     doc.Latitude.String(),
		Longitude:    doc.Longitude.String(),
		Organization: cleanIPMeValue(doc.Organization),
		ISP:          cleanIPMeValue(doc.ISP),
		ASN:          normalizeASN(doc.ASN.String()),
		Source:       rawURL,
	}
	if !out.hasAttribution() {
		return out, fmt.Errorf("ip.sb 未返回归属")
	}
	return out, nil
}

func lookupIPInfo(ctx context.Context, client *http.Client, query string) (ipMeOutput, error) {
	rawURL := "https://ipinfo.io/json"
	if query != "" {
		rawURL = "https://ipinfo.io/" + query + "/json"
	}
	body, err := getText(ctx, client, rawURL, curlUA, "application/json")
	if err != nil {
		return ipMeOutput{}, err
	}
	var doc struct {
		IP      string `json:"ip"`
		City    string `json:"city"`
		Country string `json:"country"`
		Loc     string `json:"loc"`
		Org     string `json:"org"`
		Postal  string `json:"postal"`
		Bogon   bool   `json:"bogon"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return ipMeOutput{}, fmt.Errorf("ipinfo.io 响应无法解析")
	}
	if doc.Bogon {
		return ipMeOutput{}, fmt.Errorf("ipinfo.io 拒绝该地址")
	}
	asn, org := splitASOrg(doc.Org)
	lat, lon := splitLoc(doc.Loc)
	out := ipMeOutput{
		IP:           doc.IP,
		City:         cleanIPMeValue(doc.City),
		CountryCode:  cleanIPMeValue(doc.Country),
		Latitude:     lat,
		Longitude:    lon,
		PostalCode:   cleanIPMeValue(doc.Postal),
		Organization: org,
		ASN:          asn,
		Source:       rawURL,
	}
	if !out.hasAttribution() {
		return out, fmt.Errorf("ipinfo.io 未返回归属")
	}
	return out, nil
}

func lookupIPWho(ctx context.Context, client *http.Client, query string) (ipMeOutput, error) {
	rawURL := "https://ipwho.is/"
	if query != "" {
		rawURL = "https://ipwho.is/" + query
	}
	body, err := getText(ctx, client, rawURL, curlUA, "application/json")
	if err != nil {
		return ipMeOutput{}, err
	}
	var doc struct {
		IP          string     `json:"ip"`
		Success     bool       `json:"success"`
		Country     string     `json:"country"`
		CountryCode string     `json:"country_code"`
		City        string     `json:"city"`
		Latitude    flexString `json:"latitude"`
		Longitude   flexString `json:"longitude"`
		Postal      string     `json:"postal"`
		Connection  struct {
			ASN flexString `json:"asn"`
			Org string     `json:"org"`
			ISP string     `json:"isp"`
		} `json:"connection"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return ipMeOutput{}, fmt.Errorf("ipwho.is 响应无法解析")
	}
	if !doc.Success {
		return ipMeOutput{}, fmt.Errorf("ipwho.is 查询失败")
	}
	out := ipMeOutput{
		IP:           doc.IP,
		City:         cleanIPMeValue(doc.City),
		Country:      cleanIPMeValue(doc.Country),
		CountryCode:  cleanIPMeValue(doc.CountryCode),
		Latitude:     doc.Latitude.String(),
		Longitude:    doc.Longitude.String(),
		PostalCode:   cleanIPMeValue(doc.Postal),
		Organization: cleanIPMeValue(doc.Connection.Org),
		ISP:          cleanIPMeValue(doc.Connection.ISP),
		ASN:          normalizeASN(doc.Connection.ASN.String()),
		Source:       rawURL,
	}
	if !out.hasAttribution() {
		return out, fmt.Errorf("ipwho.is 未返回归属")
	}
	return out, nil
}

func lookupIfconfigCo(ctx context.Context, client *http.Client, query string) (ipMeOutput, error) {
	rawURL := "https://ifconfig.co/json"
	if query != "" {
		rawURL = "https://ifconfig.co/json?ip=" + url.QueryEscape(query)
	}
	body, err := getText(ctx, client, rawURL, curlUA, "application/json")
	if err != nil {
		return ipMeOutput{}, err
	}
	var doc struct {
		IP         string     `json:"ip"`
		Country    string     `json:"country"`
		CountryISO string     `json:"country_iso"`
		City       string     `json:"city"`
		Latitude   flexString `json:"latitude"`
		Longitude  flexString `json:"longitude"`
		ASN        string     `json:"asn"`
		ASNOrg     string     `json:"asn_org"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return ipMeOutput{}, fmt.Errorf("ifconfig.co 响应无法解析")
	}
	out := ipMeOutput{
		IP:           doc.IP,
		Country:      cleanIPMeValue(doc.Country),
		CountryCode:  cleanIPMeValue(doc.CountryISO),
		City:         cleanIPMeValue(doc.City),
		Latitude:     doc.Latitude.String(),
		Longitude:    doc.Longitude.String(),
		Organization: cleanIPMeValue(doc.ASNOrg),
		ASN:          normalizeASN(doc.ASN),
		Source:       rawURL,
	}
	if !out.hasAttribution() {
		return out, fmt.Errorf("ifconfig.co 未返回归属")
	}
	return out, nil
}

func lookupIdentMe(ctx context.Context, client *http.Client, query string) (ipMeOutput, error) {
	if query != "" {
		return ipMeOutput{}, fmt.Errorf("ident.me 不能查询指定 IP")
	}
	rawURL := "https://ident.me/json"
	body, err := getText(ctx, client, rawURL, curlUA, "application/json")
	if err != nil {
		return ipMeOutput{}, err
	}
	var doc struct {
		IP        string     `json:"ip"`
		ASO       string     `json:"aso"`
		ASN       flexString `json:"asn"`
		Country   string     `json:"country"`
		CC        string     `json:"cc"`
		City      string     `json:"city"`
		Postal    string     `json:"postal"`
		Latitude  flexString `json:"latitude"`
		Longitude flexString `json:"longitude"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return ipMeOutput{}, fmt.Errorf("ident.me 响应无法解析")
	}
	out := ipMeOutput{
		IP:           doc.IP,
		City:         cleanIPMeValue(doc.City),
		Country:      cleanIPMeValue(doc.Country),
		CountryCode:  cleanIPMeValue(doc.CC),
		Latitude:     doc.Latitude.String(),
		Longitude:    doc.Longitude.String(),
		PostalCode:   cleanIPMeValue(doc.Postal),
		Organization: cleanIPMeValue(doc.ASO),
		ASN:          normalizeASN(doc.ASN.String()),
		Source:       rawURL,
	}
	if !out.hasAttribution() {
		return out, fmt.Errorf("ident.me 未返回归属")
	}
	return out, nil
}

func lookupIPAPI(ctx context.Context, client *http.Client, query string) (ipMeOutput, error) {
	rawURL := "http://ip-api.com/json?fields=status,message,country,countryCode,city,lat,lon,zip,isp,org,as,query"
	if query != "" {
		rawURL = "http://ip-api.com/json/" + query + "?fields=status,message,country,countryCode,city,lat,lon,zip,isp,org,as,query"
	}
	body, err := getText(ctx, client, rawURL, curlUA, "application/json")
	if err != nil {
		return ipMeOutput{}, err
	}
	var doc struct {
		Status      string     `json:"status"`
		Message     string     `json:"message"`
		Country     string     `json:"country"`
		CountryCode string     `json:"countryCode"`
		City        string     `json:"city"`
		Lat         flexString `json:"lat"`
		Lon         flexString `json:"lon"`
		Zip         string     `json:"zip"`
		ISP         string     `json:"isp"`
		Org         string     `json:"org"`
		AS          string     `json:"as"`
		Query       string     `json:"query"`
	}
	if err := json.Unmarshal([]byte(body), &doc); err != nil {
		return ipMeOutput{}, fmt.Errorf("ip-api.com 响应无法解析")
	}
	if doc.Status != "success" {
		if doc.Message == "" {
			doc.Message = "查询失败"
		}
		return ipMeOutput{}, fmt.Errorf("ip-api.com %s", doc.Message)
	}
	asn, _ := splitASOrg(doc.AS)
	out := ipMeOutput{
		IP:           doc.Query,
		City:         cleanIPMeValue(doc.City),
		Country:      cleanIPMeValue(doc.Country),
		CountryCode:  cleanIPMeValue(doc.CountryCode),
		Latitude:     doc.Lat.String(),
		Longitude:    doc.Lon.String(),
		PostalCode:   cleanIPMeValue(doc.Zip),
		Organization: cleanIPMeValue(doc.Org),
		ISP:          cleanIPMeValue(doc.ISP),
		ASN:          asn,
		Source:       rawURL,
	}
	if !out.hasAttribution() {
		return out, fmt.Errorf("ip-api.com 未返回归属")
	}
	return out, nil
}

func normalizeASN(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.EqualFold(s[:2], "AS") {
		s = strings.TrimSpace(s[2:])
	}
	return cleanIPMeValue(s)
}

func splitASOrg(org string) (asn, name string) {
	org = strings.TrimSpace(org)
	if len(org) >= 2 && strings.EqualFold(org[:2], "AS") {
		head, rest, _ := strings.Cut(org, " ")
		return normalizeASN(head), cleanIPMeValue(rest)
	}
	return "", cleanIPMeValue(org)
}

func splitLoc(loc string) (lat, lon string) {
	lat, lon, ok := strings.Cut(loc, ",")
	if !ok {
		return "", ""
	}
	return cleanIPMeValue(lat), cleanIPMeValue(lon)
}
