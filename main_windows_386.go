//go:build windows && 386

package main

import _ "embed"

//go:embed assets/wintun_386.dll
var wintunDLL []byte
