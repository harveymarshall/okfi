package cli

import "testing"

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantPrefix string
		wantErr    bool
	}{
		{
			name:       "bucket and prefix, no trailing slash",
			uri:        "s3://my-bucket/some/path",
			wantBucket: "my-bucket",
			// Normalized with a trailing slash so S3's plain string-prefix
			// match can't cross into unrelated siblings like
			// "some/path-other/" — only true descendants of "some/path/".
			wantPrefix: "some/path/",
		},
		{
			name:       "bucket and prefix, trailing slash already present",
			uri:        "s3://my-bucket/some/path/",
			wantBucket: "my-bucket",
			wantPrefix: "some/path/",
		},
		{
			name:       "bare bucket, no prefix",
			uri:        "s3://my-bucket",
			wantBucket: "my-bucket",
			wantPrefix: "",
		},
		{
			name:    "missing scheme",
			uri:     "my-bucket/some/path",
			wantErr: true,
		},
		{
			name:    "missing bucket",
			uri:     "s3://",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bucket, prefix, err := parseS3URI(tt.uri)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got bucket=%q prefix=%q", bucket, prefix)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if bucket != tt.wantBucket {
				t.Errorf("bucket = %q, want %q", bucket, tt.wantBucket)
			}
			if prefix != tt.wantPrefix {
				t.Errorf("prefix = %q, want %q", prefix, tt.wantPrefix)
			}
		})
	}
}
