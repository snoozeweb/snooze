package sns

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestSigV4GetVanilla validates the hand-rolled signer against the AWS
// "Signature Version 4 Test Suite" `get-vanilla` vector. These exact inputs
// and the resulting signature/Authorization are documented by AWS; the
// expected values below were independently reproduced with Python's stdlib
// hmac/hashlib (see the package notes) so they pin the algorithm, not our own
// implementation.
//
//	access key id : AKIDEXAMPLE
//	secret key    : wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY
//	region        : us-east-1
//	service       : service
//	x-amz-date    : 20150830T123600Z
//	host          : example.amazonaws.com
//	method        : GET   uri "/"   no query   empty body
func TestSigV4GetVanilla(t *testing.T) {
	res := sign(signParams{
		Method:  "GET",
		Host:    "example.amazonaws.com",
		Path:    "/",
		Query:   "",
		Body:    nil,
		Region:  "us-east-1",
		Service: "service",
		Creds: credentials{
			AccessKeyID:     "AKIDEXAMPLE",
			SecretAccessKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY",
		},
		Now: time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC),
	})

	const wantSignature = "5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"

	require.Equal(t,
		"GET\n/\n\nhost:example.amazonaws.com\nx-amz-date:20150830T123600Z\n\nhost;x-amz-date\n"+
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		res.CanonicalRequest, "canonical request must match the documented vector")

	require.Equal(t,
		"AWS4-HMAC-SHA256\n20150830T123600Z\n20150830/us-east-1/service/aws4_request\n"+
			"bb579772317eb040ac9ed261061d46c1f17a8133879d6129b6e1c25292927e63",
		res.StringToSign, "string-to-sign must match the documented vector")

	require.Equal(t, wantSignature, res.Signature, "signature must match the AWS get-vanilla vector")
	require.Equal(t, "20150830T123600Z", res.AmzDate)
	require.Equal(t, "host;x-amz-date", res.SignedHeaders)
	require.Equal(t,
		"AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, "+
			"SignedHeaders=host;x-amz-date, Signature="+wantSignature,
		res.Authorization, "Authorization header must match the documented vector")
}

// TestSigV4SessionTokenIsSigned verifies that supplying a session token folds
// x-amz-security-token into the signed headers (it must appear, in sorted
// position, in both the canonical headers and SignedHeaders list).
func TestSigV4SessionTokenIsSigned(t *testing.T) {
	res := sign(signParams{
		Method:  "POST",
		Host:    "sns.eu-west-1.amazonaws.com",
		Path:    "/",
		Headers: map[string]string{"content-type": "application/x-www-form-urlencoded"},
		Body:    []byte("Action=Publish"),
		Region:  "eu-west-1",
		Service: "sns",
		Creds: credentials{
			AccessKeyID:     "AKIDEXAMPLE",
			SecretAccessKey: "secret",
			SessionToken:    "tok-123",
		},
		Now: time.Date(2026, 5, 27, 0, 0, 0, 0, time.UTC),
	})

	require.Equal(t, "content-type;host;x-amz-date;x-amz-security-token", res.SignedHeaders)
	require.Contains(t, res.CanonicalRequest, "x-amz-security-token:tok-123\n")
	require.Equal(t, "tok-123", res.SecurityToken)
}

// TestSigV4DefaultsNow ensures a zero Now does not panic and produces a
// well-formed amz date (smoke test for the time.Now() fallback path).
func TestSigV4DefaultsNow(t *testing.T) {
	res := sign(signParams{
		Method:  "POST",
		Host:    "sns.us-east-1.amazonaws.com",
		Region:  "us-east-1",
		Service: "sns",
		Creds:   credentials{AccessKeyID: "AKID", SecretAccessKey: "secret"},
	})
	require.Len(t, res.AmzDate, len("20060102T150405Z"))
	require.Contains(t, res.Authorization, "AWS4-HMAC-SHA256 Credential=AKID/")
}
