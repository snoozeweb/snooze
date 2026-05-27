package sns

// AWS Signature Version 4 signer, hand-rolled on the standard library
// (crypto/hmac, crypto/sha256, encoding/hex). No AWS SDK.
//
// The implementation follows the documented SigV4 algorithm:
//
//	1. canonical request  = method \n uri \n query \n headers \n signed \n hash(body)
//	2. string to sign      = "AWS4-HMAC-SHA256" \n amzDate \n scope \n hash(canonicalRequest)
//	3. signing key         = HMAC chain over "AWS4"+secret → date → region → service → "aws4_request"
//	4. signature           = hex(HMAC(signingKey, stringToSign))
//	5. Authorization header = "AWS4-HMAC-SHA256 Credential=…, SignedHeaders=…, Signature=…"
//
// It is validated in sigv4_test.go against the AWS SigV4 Test Suite
// "get-vanilla" vector, so it is safe to reuse for any GET/POST request, not
// just SNS Publish.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"
)

// sigV4Algorithm is the only algorithm this signer implements.
const sigV4Algorithm = "AWS4-HMAC-SHA256"

// credentials carries the AWS credentials used to sign a request. SessionToken
// is set only for temporary (STS) credentials; when present it is folded into
// the signature via the x-amz-security-token header.
type credentials struct {
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// signParams fully describes one request to sign. The signer never inspects an
// *http.Request directly so the algorithm stays testable in isolation.
type signParams struct {
	Method  string            // HTTP method, e.g. "POST"
	Host    string            // value of the host header (no scheme, no path)
	Path    string            // canonical URI; "/" when empty
	Query   string            // canonical (already-sorted/encoded) query string; "" for none
	Headers map[string]string // extra signed headers beyond host/x-amz-date (e.g. content-type)
	Body    []byte            // request payload (hashed; may be empty)
	Region  string
	Service string
	Creds   credentials
	Now     time.Time // request time; defaults to time.Now().UTC() when zero
}

// signResult is what sign() returns: the headers a caller must set on the wire.
type signResult struct {
	AmzDate       string // X-Amz-Date  (e.g. 20150830T123600Z)
	Authorization string // full Authorization header value
	SecurityToken string // X-Amz-Security-Token; empty when no session token

	// Exposed for tests / debugging: the intermediate strings.
	CanonicalRequest string
	StringToSign     string
	Signature        string
	SignedHeaders    string
}

// sign computes the SigV4 signature and the headers needed to send the request.
func sign(p signParams) signResult {
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	path := p.Path
	if path == "" {
		path = "/"
	}

	// Assemble the headers that participate in the signature. host and
	// x-amz-date are always signed; x-amz-security-token is signed when a
	// session token is present; any caller-supplied headers (e.g.
	// content-type) are signed too. Keys are lowercased; values trimmed.
	headers := map[string]string{
		"host":       p.Host,
		"x-amz-date": amzDate,
	}
	if p.Creds.SessionToken != "" {
		headers["x-amz-security-token"] = p.Creds.SessionToken
	}
	for k, v := range p.Headers {
		headers[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}

	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)

	var canonicalHeaders strings.Builder
	for _, k := range names {
		canonicalHeaders.WriteString(k)
		canonicalHeaders.WriteByte(':')
		canonicalHeaders.WriteString(headers[k])
		canonicalHeaders.WriteByte('\n')
	}
	signedHeaders := strings.Join(names, ";")

	payloadHash := hexSHA256(p.Body)

	canonicalRequest := strings.Join([]string{
		p.Method,
		path,
		p.Query,
		canonicalHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := strings.Join([]string{dateStamp, p.Region, p.Service, "aws4_request"}, "/")

	stringToSign := strings.Join([]string{
		sigV4Algorithm,
		amzDate,
		scope,
		hexSHA256([]byte(canonicalRequest)),
	}, "\n")

	signingKey := signingKey(p.Creds.SecretAccessKey, dateStamp, p.Region, p.Service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	authorization := sigV4Algorithm +
		" Credential=" + p.Creds.AccessKeyID + "/" + scope +
		", SignedHeaders=" + signedHeaders +
		", Signature=" + signature

	return signResult{
		AmzDate:          amzDate,
		Authorization:    authorization,
		SecurityToken:    p.Creds.SessionToken,
		CanonicalRequest: canonicalRequest,
		StringToSign:     stringToSign,
		Signature:        signature,
		SignedHeaders:    signedHeaders,
	}
}

// signingKey derives the SigV4 signing key by the documented HMAC chain.
func signingKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// hmacSHA256 returns HMAC-SHA256(key, data).
func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// hexSHA256 returns the lowercase hex of SHA256(data).
func hexSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
