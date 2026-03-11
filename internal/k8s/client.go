package k8s

import (
	"context"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"k8s.io/client-go/rest"

	"github.com/geraldofigueiredo/kportmaster/internal/gcp"
)

// BuildRESTConfig constructs a *rest.Config from a GCP Cluster struct using ADC.
func BuildRESTConfig(ctx context.Context, cluster gcp.Cluster) (*rest.Config, error) {
	return BuildRESTConfigFromParts(ctx, cluster.Endpoint, cluster.CAData)
}

// BuildRESTConfigFromParts builds a *rest.Config from raw endpoint + CA data using ADC.
// Used to resume tunnels loaded from history without re-fetching cluster info from GCP.
func BuildRESTConfigFromParts(ctx context.Context, endpoint string, caData []byte) (*rest.Config, error) {
	creds, err := google.FindDefaultCredentials(ctx,
		"https://www.googleapis.com/auth/cloud-platform",
	)
	if err != nil {
		return nil, fmt.Errorf("finding default credentials: %w", err)
	}

	ts := creds.TokenSource
	return &rest.Config{
		Host: "https://" + endpoint,
		TLSClientConfig: rest.TLSClientConfig{
			CAData: caData,
		},
		WrapTransport: func(rt http.RoundTripper) http.RoundTripper {
			return &oauth2.Transport{Source: ts, Base: rt}
		},
	}, nil
}
