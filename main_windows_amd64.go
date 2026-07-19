//go:build windows && amd64

package main

import _ "embed"

//go:embed assets/wintun_amd64.dll
var wintunDLL []byte
