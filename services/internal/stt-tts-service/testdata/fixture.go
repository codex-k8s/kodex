// Package testdata содержит разрешённую владельцем переносимую STT-фикстуру.
package testdata

import _ "embed"

//go:embed 1-2-3-4-5.mp3
var RussianNumbers []byte
