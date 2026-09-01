package inbound

// Inbound authentication: how a caller proves it is the hook's bound
// credential. Three modes (config.Auth*), two of them signatures.
// Signature verification is preferred over a bearer wherever the source
// can sign: the secret never travels, the body is bound to the proof,
// and the signed timestamp (plus the signed delivery id in Kaimahi's own
// scheme) is what makes replay protection more than a database index.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// Kaimahi's generic signed-webhook scheme (config.AuthKaimahiHMAC):
	//   X-Kaimahi-Delivery:  the source's unique delivery id
	//   X-Kaimahi-Timestamp: unix seconds at signing time
	//   X-Kaimahi-Signature: v1=<hex hmac-sha256 over "v1:<ts>:<delivery>:<body>">
	// A bearer hook (config.AuthBearer) sends only the delivery header.
	HeaderDelivery  = "X-Kaimahi-Delivery"
	HeaderTimestamp = "X-Kaimahi-Timestamp"
	HeaderSignature = "X-Kaimahi-Signature"
	kaimahiVersion  = "v1"

	// Slack request signing (config.AuthSlack): v0=<hex hmac-sha256 over
	// "v0:<ts>:<body>">; the delivery id is the signed body's event_id.
	slackTimestamp = "X-Slack-Request-Timestamp"
	slackSignature = "X-Slack-Signature"
	slackVersion   = "v0"

	// replayWindow bounds how old a signed timestamp may be (Slack's own
	// recommendation is five minutes). Beyond it the signature is stale
	// and refused even if it verifies.
	replayWindow = 5 * time.Minute
)

// deliveryRe bounds a delivery id: it lands in an audit row and a unique
// index, so keep it a plain identifier of sane length.
var deliveryRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

var errSecretUnavailable = errors.New("signing secret unavailable")

// readSecret reads a hook's signing secret per request from plane-side
// custody (a Secret-mounted file), so rotation needs no restart. Missing
// or empty fails closed — the caller answers 503, never "unsigned".
func readSecret(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errSecretUnavailable
	}
	secret := strings.TrimSpace(string(raw))
	if secret == "" {
		return nil, errSecretUnavailable
	}
	return []byte(secret), nil
}

// verifySignature checks "<version>=<hex>" against HMAC-SHA256(secret,
// base) in constant time.
func verifySignature(secret []byte, header, version string, base []byte) bool {
	sigHex, ok := strings.CutPrefix(header, version+"=")
	if !ok {
		return false
	}
	got, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(base)
	return hmac.Equal(got, mac.Sum(nil))
}

// freshTimestamp parses a signed unix-seconds timestamp and requires it
// to be within replayWindow of now (either direction — clock skew is
// symmetric).
func freshTimestamp(ts string, now time.Time) bool {
	n, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	d := now.Sub(time.Unix(n, 0))
	return d <= replayWindow && d >= -replayWindow
}

func kaimahiBase(ts, delivery string, body []byte) []byte {
	return []byte(kaimahiVersion + ":" + ts + ":" + delivery + ":" + string(body))
}

func slackBase(ts string, body []byte) []byte {
	return []byte(slackVersion + ":" + ts + ":" + string(body))
}

// Sign produces Kaimahi's v1 signature for a request — exported so the
// probe/test tooling and any Go caller sign exactly what the plane
// verifies.
func Sign(secret []byte, ts, delivery string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(kaimahiBase(ts, delivery, body))
	return kaimahiVersion + "=" + hex.EncodeToString(mac.Sum(nil))
}

// event is what the bridge extracts from a verified payload: the text
// the agent is asked, and (for sources that carry it in the body) the
// delivery id. challenge is Slack's URL-verification handshake — a
// payload that must be echoed, never acted on.
type event struct {
	text      string
	delivery  string
	challenge string
}

// genericEvent takes the text from a JSON object's "text" field when
// there is one, otherwise the body itself as text. Anything that is not
// UTF-8 text is refused: a webhook payload becomes a prompt, and a
// prompt is text.
func genericEvent(body []byte) (event, bool) {
	if !utf8.Valid(body) {
		return event{}, false
	}
	var m struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(body, &m) == nil && m.Text != nil {
		return event{text: *m.Text}, *m.Text != ""
	}
	text := strings.TrimSpace(string(body))
	return event{text: text}, text != ""
}

// slackEvent understands the two Events API envelope shapes that matter:
// url_verification (answer the challenge) and event_callback (the inner
// event's text, keyed by the envelope's event_id). Anything else is
// refused: Slack sends nothing else to an events URL, and an unknown
// shape must not become a prompt.
func slackEvent(body []byte) (event, bool) {
	var env struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		EventID   string `json:"event_id"`
		Event     struct {
			Text string `json:"text"`
		} `json:"event"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return event{}, false
	}
	switch env.Type {
	case "url_verification":
		return event{challenge: env.Challenge}, env.Challenge != ""
	case "event_callback":
		text := strings.TrimSpace(env.Event.Text)
		return event{text: text, delivery: env.EventID}, text != "" && env.EventID != ""
	}
	return event{}, false
}
