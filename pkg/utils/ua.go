package utils

import "strings"

var osList = []struct{ key, name string }{
	{"Windows NT 10", "Windows 10/11"},
	{"Windows NT 6.3", "Windows 8.1"},
	{"Windows NT 6.1", "Windows 7"},
	{"Mac OS X", "macOS"},
	{"Android", "Android"},
	{"iPhone", "iOS"},
	{"iPad", "iPadOS"},
	{"Linux", "Linux"},
}

var browserList = []struct{ key, name string }{
	{"Edg/", "Edge"},
	{"OPR/", "Opera"},
	{"Chrome/", "Chrome"},
	{"Safari/", "Safari"},
	{"Firefox/", "Firefox"},
	{"MSIE", "IE"},
	{"Trident", "IE"},
	{"curl/", "curl"},
	{"PostmanRuntime", "Postman"},
	{"Go-http-client", "Go"},
}

// ParseUA 解析 User-Agent，返回浏览器与操作系统名称。
func ParseUA(ua string) (browser, os string) {
	browser, os = "Unknown", "Unknown"
	for _, b := range browserList {
		if strings.Contains(ua, b.key) {
			browser = b.name
			break
		}
	}
	for _, o := range osList {
		if strings.Contains(ua, o.key) {
			os = o.name
			break
		}
	}
	return browser, os
}
