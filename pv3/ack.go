package pv3

// @brief The fixed 84-byte reply to a Joan hello.
//
// @details It (re)establishes the session before any frame is pushed and carries the device UUID and
// display parameters. The CRC is pre-computed. The constant is decoded once at init and panics when
// it is invalid.
var SessionACK = mustDecodeHex(
	"03000000000000000100000040000000ee13318c" +
		"0000000000000000280000002c0000000000000000000000f00100000000" +
		"420028000d5137313734393410001301040010080d00b000000001000000" +
		"00000000")
