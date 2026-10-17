package media

import (
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Vectors from AWS's documentation: the signature version 4 test suite (get-vanilla), the key derivation
// example, and the Amazon S3 header-based authentication examples.

func TestSigningKeyVector(t *testing.T) {
	key := signingKey("wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY", time.Date(2015, 8, 30, 0, 0, 0, 0, time.UTC), "us-east-1", "iam")
	if got := hex.EncodeToString(key); got != "c4afb1cc5771d871763a393e44b703571b55cc28424d1a5e86da6ed3c154a4b9" {
		t.Fatalf("signing key = %s", got)
	}
}

func TestGetVanilla(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://example.amazonaws.com/", nil)
	now := time.Date(2015, 8, 30, 12, 36, 0, 0, time.UTC)
	c := credentials{AccessKey: "AKIDEXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"}
	signV4(req, emptySHA256, c, "us-east-1", "service", now)

	canonical, signed := canonicalRequest(req, emptySHA256)
	wantCanonical := "GET\n/\n\nhost:example.amazonaws.com\nx-amz-date:20150830T123600Z\n\nhost;x-amz-date\n" + emptySHA256
	if canonical != wantCanonical || signed != "host;x-amz-date" {
		t.Fatalf("canonical request:\n%s", canonical)
	}
	sts := stringToSign("20150830T123600Z", scopeOf(now, "us-east-1", "service"), canonical)
	if want := "AWS4-HMAC-SHA256\n20150830T123600Z\n20150830/us-east-1/service/aws4_request\nbb579772317eb040ac9ed261061d46c1f17a8133879d6129b6e1c25292927e63"; sts != want {
		t.Fatalf("string to sign:\n%s", sts)
	}
	want := "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/20150830/us-east-1/service/aws4_request, SignedHeaders=host;x-amz-date, " +
		"Signature=5fa00fa31553b73ebf1942676e86291e8372ff2a2260956d9b8aae1d763fbf31"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization = %s", got)
	}
}

var s3Example = credentials{AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"}

func TestS3GetObjectWithRange(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	req.Header.Set("X-Amz-Content-Sha256", emptySHA256)
	now := time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)
	signV4(req, emptySHA256, s3Example, "us-east-1", "s3", now)

	canonical, _ := canonicalRequest(req, emptySHA256)
	want := "GET\n/test.txt\n\nhost:examplebucket.s3.amazonaws.com\nrange:bytes=0-9\nx-amz-content-sha256:" + emptySHA256 +
		"\nx-amz-date:20130524T000000Z\n\nhost;range;x-amz-content-sha256;x-amz-date\n" + emptySHA256
	if canonical != want {
		t.Fatalf("canonical request:\n%s", canonical)
	}
	if !strings.Contains(req.Header.Get("Authorization"), "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41") {
		t.Fatalf("Authorization = %s", req.Header.Get("Authorization"))
	}
}

func TestS3PutObject(t *testing.T) {
	const payload = "44ce7dd67c959e0d3524ffac1771dfbba87d2b6b4b4e99e42034a8b803f8b072" // SHA-256 of "Welcome to Amazon S3."
	req, _ := http.NewRequest(http.MethodPut, "https://examplebucket.s3.amazonaws.com/test$file.text", nil)
	req.Header.Set("Date", "Fri, 24 May 2013 00:00:00 GMT")
	req.Header.Set("X-Amz-Storage-Class", "REDUCED_REDUNDANCY")
	req.Header.Set("X-Amz-Content-Sha256", payload)
	signV4(req, payload, s3Example, "us-east-1", "s3", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(req.Header.Get("Authorization"), "SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class") ||
		!strings.Contains(req.Header.Get("Authorization"), "Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd") {
		t.Fatalf("Authorization = %s", req.Header.Get("Authorization"))
	}
}

func TestS3ListObjects(t *testing.T) {
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/?max-keys=2&prefix=J", nil)
	req.Header.Set("X-Amz-Content-Sha256", emptySHA256)
	signV4(req, emptySHA256, s3Example, "us-east-1", "s3", time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))
	if !strings.Contains(req.Header.Get("Authorization"), "Signature=34b48302e7b5fa45bde8084f4b7868a86f0a534bc59db6670ed5711ef69dc6f7") {
		t.Fatalf("Authorization = %s", req.Header.Get("Authorization"))
	}
}

func TestURIEncode(t *testing.T) {
	for in, want := range map[string]string{"a b": "a%20b", "a/b": "a/b", "é": "%C3%A9", "~-_.": "~-_.", "a+b=c&d": "a%2Bb%3Dc%26d"} {
		if got := uriEncode(in, false); got != want {
			t.Errorf("uriEncode(%q) = %q, want %q", in, got, want)
		}
	}
	if got := uriEncode("a/b", true); got != "a%2Fb" {
		t.Errorf("slash encoded: %q", got)
	}
}
