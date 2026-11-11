package internal

import (
	"testing"

	token_pb "github.com/tacplus-dev/opa/tacplus/types/sys/token"
)

// A numeric token_use is accepted only when it is an integer in int32 range
// and a defined TokenUse value (#12): converting anything else truncated it,
// so 2^32 + 6 or 6.5 came out as TOKEN_USE_EXTERNAL.
func TestParseTokenUseNumbers(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  interface{}
		want token_pb.TokenUse
	}{
		{"int64 external", int64(6), token_pb.TokenUse_TOKEN_USE_EXTERNAL},
		{"float64 external", float64(6), token_pb.TokenUse_TOKEN_USE_EXTERNAL},
		{"int64 over int32", int64(1<<32 + 6), token_pb.TokenUse_TOKEN_USE_UNSPECIFIED},
		{"float64 over int32", float64(1<<32 + 6), token_pb.TokenUse_TOKEN_USE_UNSPECIFIED},
		{"int64 under int32", int64(-1<<32 + 6), token_pb.TokenUse_TOKEN_USE_UNSPECIFIED},
		{"float64 fraction", 6.5, token_pb.TokenUse_TOKEN_USE_UNSPECIFIED},
		{"int64 undefined", int64(9999), token_pb.TokenUse_TOKEN_USE_UNSPECIFIED},
		{"float64 undefined", float64(9999), token_pb.TokenUse_TOKEN_USE_UNSPECIFIED},
	} {
		if got := parseTokenUse(tc.raw); got != tc.want {
			t.Errorf("%s: parseTokenUse(%v) = %v, want %v", tc.name, tc.raw, got, tc.want)
		}
	}
}
