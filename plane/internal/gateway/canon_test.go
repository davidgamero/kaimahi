package gateway

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A duplicated key is a tampering signal at EVERY depth: Go reads
// last-wins, an upstream parser may read first-wins, and once arguments
// are policy inputs that difference is a smuggling vector. The message
// is refused, not collapsed.
func TestCanonicalizeRefusesDuplicateKeysAtEveryDepth(t *testing.T) {
	cases := map[string]string{
		"top level": `{"jsonrpc":"2.0","id":1,"method":"tools/call","method":"tools/list"}`,
		"params":    `{"method":"tools/call","params":{"name":"a","name":"b"}}`,
		"arguments": `{"method":"tools/call","params":{"name":"pay","arguments":{"amount":42000,"amount":48000}}}`,
		"deeper still": `{"method":"tools/call","params":{"name":"pay","arguments":` +
			`{"invoice":{"line":{"amount":1,"amount":2}}}}}`,
		"inside an array": `{"method":"tools/call","params":{"name":"pay","arguments":` +
			`{"lines":[{"ok":1},{"amount":1,"amount":2}]}}}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := canonicalize([]byte(body))
			require.ErrorIs(t, err, errDuplicateKey)
		})
	}
}

func TestCanonicalizeAcceptsTheSameKeyInSiblingObjects(t *testing.T) {
	body := `{"method":"tools/call","params":{"name":"pay","arguments":{"a":{"x":1},"b":{"x":2}}}}`
	out, msg, err := canonicalize([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, "tools/call", msg["method"])
	assert.JSONEq(t, body, string(out))
}

// One normalized representation: the bytes forwarded upstream are the
// re-marshalled tree enforcement was decided on, so the two can never
// disagree. Keys come out sorted and whitespace-free; order inside
// arrays, and every value, survive.
func TestCanonicalizeIsOneRepresentationForEveryConsumer(t *testing.T) {
	body := `{ "method" : "tools/call" , "id": 1, "params": {"arguments": {
		"z": [3, {"b": true, "a": null}, "kōrero"], "a": "x"}, "name": "pay"} }`
	out, msg, err := canonicalize([]byte(body))
	require.NoError(t, err)
	assert.Equal(t, `{"id":1,"method":"tools/call","params":{"arguments":`+
		`{"a":"x","z":[3,{"a":null,"b":true},"kōrero"]},"name":"pay"}}`, string(out))
	// The tree the gateway inspects is the tree those bytes came from.
	args, ok := argumentsOf(msg)
	require.True(t, ok)
	assert.Equal(t, "x", args["a"])

	// Canonical bytes are a fixed point: re-canonicalizing changes nothing.
	again, _, err := canonicalize(out)
	require.NoError(t, err)
	assert.Equal(t, string(out), string(again))
}

func TestCanonicalizeNormalizesIntegerLiterals(t *testing.T) {
	out, _, err := canonicalize([]byte(`{"method":"m","params":{"a":-0,"b":48000,"c":1e2,"d":1.50}}`))
	require.NoError(t, err)
	// Integer literals get one canonical form; anything else keeps the
	// literal it arrived as (see canon.go on why).
	assert.Equal(t, `{"method":"m","params":{"a":0,"b":48000,"c":1e2,"d":1.50}}`, string(out))
}

// Bounds fail closed rather than recursing (or allocating) unboundedly.
func TestCanonicalizeBoundsDepthAndSize(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxCanonDepth+2) + `1` + strings.Repeat(`}`, maxCanonDepth+2)
	_, _, err := canonicalize([]byte(`{"method":"m","params":` + deep + `}`))
	require.ErrorIs(t, err, errTooDeep)

	// Arrays nest too, and each element counts against the node budget.
	var b strings.Builder
	b.WriteString(`{"method":"m","params":{"arguments":{"lines":[`)
	for i := 0; i < maxCanonNodes+10; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("1")
	}
	b.WriteString(`]}}}`)
	_, _, err = canonicalize([]byte(b.String()))
	require.ErrorIs(t, err, errTooLarge)
}

func TestCanonicalizeRefusesNonMessages(t *testing.T) {
	for name, body := range map[string]string{
		"not json":        `{"method":`,
		"trailing data":   `{"method":"m"} {"method":"n"}`,
		"not an object":   `["tools/call"]`,
		"bare scalar":     `42`,
		"empty":           ``,
		"nan is not json": `{"a":NaN}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := canonicalize([]byte(body))
			require.Error(t, err)
			assert.NotErrorIs(t, err, errDuplicateKey)
		})
	}
}

// The canonical tree is what argument inspection reads, so a tools/call
// with no arguments reads as an empty set rather than as a failure.
func TestArgumentsOf(t *testing.T) {
	_, msg, err := canonicalize([]byte(`{"method":"tools/call","params":{"name":"pay"}}`))
	require.NoError(t, err)
	args, ok := argumentsOf(msg)
	assert.True(t, ok)
	assert.Empty(t, args)

	_, msg, err = canonicalize([]byte(`{"method":"tools/call","params":{"name":"pay","arguments":[1]}}`))
	require.NoError(t, err)
	_, ok = argumentsOf(msg)
	assert.False(t, ok, "arguments must be an object")
}

func TestToolNameOf(t *testing.T) {
	_, msg, err := canonicalize([]byte(`{"method":"tools/call","params":{"name":"pay"}}`))
	require.NoError(t, err)
	assert.Equal(t, "pay", toolNameOf(msg))

	_, msg, err = canonicalize([]byte(`{"method":"tools/call","params":{"name":42}}`))
	require.NoError(t, err)
	assert.Equal(t, "", toolNameOf(msg))
}
