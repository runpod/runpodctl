package nvtransfer

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// A truncated listing must either advance or fail, never silently finish.
type stalledListing struct {
	S3API
	t     *testing.T
	next  *string
	calls int
}

func (s *stalledListing) checkCalls() {
	s.t.Helper()
	s.calls++
	if s.calls > 2 {
		s.t.Fatal("pagination did not stop")
	}
}

func (s *stalledListing) ListObjectsV2(context.Context, *s3.ListObjectsV2Input, ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	s.checkCalls()
	return &s3.ListObjectsV2Output{IsTruncated: aws.Bool(true), NextContinuationToken: s.next}, nil
}

func (s *stalledListing) ListParts(context.Context, *s3.ListPartsInput, ...func(*s3.Options)) (*s3.ListPartsOutput, error) {
	s.checkCalls()
	return &s3.ListPartsOutput{
		IsTruncated: aws.Bool(true), NextPartNumberMarker: s.next,
		Parts: []types.Part{{PartNumber: aws.Int32(1)}},
	}, nil
}

func TestIncompleteListingsFailWithoutLooping(t *testing.T) {
	for _, listing := range []string{"objects", "parts"} {
		for _, token := range []struct {
			name string
			next *string
		}{{"missing", nil}, {"empty", aws.String("")}, {"repeated", aws.String("1")}} {
			t.Run(listing+"/"+token.name, func(t *testing.T) {
				client := &stalledListing{t: t, next: token.next}
				r := newRun(&Volume{Client: client, ID: "vol"})
				var err error
				if listing == "objects" {
					_, err = r.listPrefix(context.Background(), "data/")
				} else {
					_, err = r.listParts(context.Background(), "data/file", "upload")
				}
				if err == nil {
					t.Fatal("an incomplete listing was accepted as complete")
				}
			})
		}
	}
}
