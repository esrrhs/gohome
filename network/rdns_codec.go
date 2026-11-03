package network

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
)

// rdns wire helpers: FrameMgr bytes ride inside DNS QNAME labels (uplink) and
// TXT/NULL RDATA (downlink). Optional AES-GCM (PSK) covers payload bytes only;
// QNAME labels, timing and sizes remain visible DNS metadata.

const (
	rdnsSessionMarker = "s"
	rdnsEmptyLabel    = "0"
	rdnsMaxLabelLen   = 63
	// rdnsMaxNameLen is the DNS wire limit for a domain name.
	rdnsMaxNameLen   = 255
	rdnsGCMNonceSize = 12
)

var rdnsB32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func rdnsNormalizeDomain(domain string) string {
	d := strings.TrimSpace(strings.ToLower(domain))
	d = strings.TrimSuffix(d, ".")
	return d
}

func rdnsQueryType(s string) uint16 {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "NULL":
		return 10 // dns.TypeNULL
	default:
		return 16 // dns.TypeTXT
	}
}

func rdnsIsTXT(qtype string) bool {
	return strings.ToUpper(strings.TrimSpace(qtype)) != "NULL"
}

// rdnsSessionID returns a short DNS-safe session label from a guid-like id.
func rdnsSessionID(id string) string {
	cleaned := make([]byte, 0, 16)
	for i := 0; i < len(id) && len(cleaned) < 16; i++ {
		c := id[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			if c >= 'A' && c <= 'Z' {
				c = c - 'A' + 'a'
			}
			cleaned = append(cleaned, c)
		}
	}
	if len(cleaned) < 8 {
		// Fallback: hash the id so we always get a stable label.
		sum := sha256.Sum256([]byte(id))
		return fmt.Sprintf("%x", sum[:8])
	}
	if len(cleaned) > 16 {
		cleaned = cleaned[:16]
	}
	return string(cleaned)
}

func rdnsDeriveKey(psk string) []byte {
	sum := sha256.Sum256([]byte(psk))
	return sum[:]
}

func rdnsSeal(psk string, plain []byte) ([]byte, error) {
	if psk == "" {
		out := make([]byte, len(plain))
		copy(out, plain)
		return out, nil
	}
	block, err := aes.NewCipher(rdnsDeriveKey(psk))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, rdnsGCMNonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plain, nil)...), nil
}

func rdnsOpen(psk string, sealed []byte) ([]byte, error) {
	if psk == "" {
		out := make([]byte, len(sealed))
		copy(out, sealed)
		return out, nil
	}
	if len(sealed) < rdnsGCMNonceSize {
		return nil, errors.New("rdns: ciphertext too short")
	}
	block, err := aes.NewCipher(rdnsDeriveKey(psk))
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := sealed[:rdnsGCMNonceSize]
	return gcm.Open(nil, nonce, sealed[rdnsGCMNonceSize:], nil)
}

func rdnsEncodePayload(raw []byte) string {
	if len(raw) == 0 {
		return rdnsEmptyLabel
	}
	enc := strings.ToLower(rdnsB32.EncodeToString(raw))
	var b strings.Builder
	for i := 0; i < len(enc); i += rdnsMaxLabelLen {
		if i > 0 {
			b.WriteByte('.')
		}
		end := i + rdnsMaxLabelLen
		if end > len(enc) {
			end = len(enc)
		}
		b.WriteString(enc[i:end])
	}
	return b.String()
}

func rdnsDecodePayload(labelsJoined string) ([]byte, error) {
	s := strings.ReplaceAll(labelsJoined, ".", "")
	if s == "" || s == rdnsEmptyLabel {
		return nil, nil
	}
	s = strings.ToUpper(s)
	return rdnsB32.DecodeString(s)
}

// rdnsBuildQName builds FQDN: <payload-labels>.s.<session>.<domain>.
func rdnsBuildQName(domain, session string, payload []byte) (string, error) {
	domain = rdnsNormalizeDomain(domain)
	session = strings.ToLower(session)
	if domain == "" || session == "" {
		return "", errors.New("rdns: domain and session required")
	}
	pl := rdnsEncodePayload(payload)
	name := pl + "." + rdnsSessionMarker + "." + session + "." + domain + "."
	if len(name) > rdnsMaxNameLen {
		return "", fmt.Errorf("rdns: qname too long (%d > %d); reduce CutSize", len(name), rdnsMaxNameLen)
	}
	return name, nil
}

// rdnsParseQName extracts session and uplink payload from a tunnel QNAME.
// Returns ok=false when the name is not under domain or lacks the session marker.
func rdnsParseQName(qname, domain string) (session string, payload []byte, ok bool, err error) {
	domain = rdnsNormalizeDomain(domain)
	name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(qname), "."))
	if domain == "" || name == "" {
		return "", nil, false, nil
	}
	suffix := "." + domain
	if name != domain && !strings.HasSuffix(name, suffix) {
		return "", nil, false, nil
	}
	var left string
	if name == domain {
		return "", nil, false, nil
	}
	left = strings.TrimSuffix(name, suffix)
	left = strings.TrimSuffix(left, ".")
	parts := strings.Split(left, ".")
	if len(parts) < 2 {
		return "", nil, false, nil
	}
	// ...payload.s.session
	session = parts[len(parts)-1]
	marker := parts[len(parts)-2]
	if marker != rdnsSessionMarker || session == "" {
		return "", nil, false, nil
	}
	dataLabels := parts[:len(parts)-2]
	joined := strings.Join(dataLabels, ".")
	payload, err = rdnsDecodePayload(joined)
	if err != nil {
		return "", nil, false, err
	}
	return session, payload, true, nil
}

// rdnsPackFrames length-prefixes one or more protobuf frame blobs into a single
// downlink/uplink byte blob: [u16be len][bytes]...
func rdnsPackFrames(frames [][]byte) []byte {
	if len(frames) == 0 {
		return nil
	}
	n := 0
	for _, f := range frames {
		n += 2 + len(f)
	}
	out := make([]byte, 0, n)
	for _, f := range frames {
		if len(f) > 0xffff {
			continue
		}
		out = append(out, byte(len(f)>>8), byte(len(f)))
		out = append(out, f...)
	}
	return out
}

func rdnsUnpackFrames(blob []byte) ([][]byte, error) {
	if len(blob) == 0 {
		return nil, nil
	}
	var out [][]byte
	i := 0
	for i+2 <= len(blob) {
		l := int(blob[i])<<8 | int(blob[i+1])
		i += 2
		if l < 0 || i+l > len(blob) {
			return nil, errors.New("rdns: truncated frame blob")
		}
		chunk := make([]byte, l)
		copy(chunk, blob[i:i+l])
		out = append(out, chunk)
		i += l
	}
	if i != len(blob) {
		return nil, errors.New("rdns: trailing bytes in frame blob")
	}
	return out, nil
}

// rdnsMaxUplinkPayload estimates how many raw bytes fit in a QNAME under domain+session.
func rdnsMaxUplinkPayload(domain, session string) int {
	domain = rdnsNormalizeDomain(domain)
	session = strings.ToLower(session)
	// "<pl>.s.<session>.<domain>." — reserve marker/session/domain/dots.
	overhead := 1 + len(rdnsSessionMarker) + 1 + len(session) + 1 + len(domain) + 1
	remain := rdnsMaxNameLen - overhead
	if remain < 1 {
		return 0
	}
	// base32 expands 5 bytes -> 8 chars; labels insert a dot every 63 chars.
	// Approximate usable base32 chars = remain minus dots between labels.
	maxLabels := (remain + rdnsMaxLabelLen) / (rdnsMaxLabelLen + 1)
	if maxLabels < 1 {
		maxLabels = 1
	}
	b32chars := remain - (maxLabels - 1) // dots between labels
	if b32chars < 1 {
		return 0
	}
	return (b32chars * 5) / 8
}
