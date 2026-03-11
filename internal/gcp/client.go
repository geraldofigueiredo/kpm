package gcp

import (
	"context"
	"encoding/base64"
	"fmt"

	"golang.org/x/oauth2/google"
	container "google.golang.org/api/container/v1"
	"google.golang.org/api/option"
)

// ListClusters uses Application Default Credentials to list all GKE clusters in a project.
func ListClusters(ctx context.Context, projectID string) ([]Cluster, error) {
	creds, err := google.FindDefaultCredentials(ctx, container.CloudPlatformScope)
	if err != nil {
		return nil, fmt.Errorf("finding default credentials: %w", err)
	}

	svc, err := container.NewService(ctx, option.WithCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("creating container service: %w", err)
	}

	resp, err := svc.Projects.Locations.Clusters.List(fmt.Sprintf("projects/%s/locations/-", projectID)).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("listing clusters for project %s: %w", projectID, err)
	}

	clusters := make([]Cluster, 0, len(resp.Clusters))
	for _, c := range resp.Clusters {
		caData, err := base64.StdEncoding.DecodeString(c.MasterAuth.ClusterCaCertificate)
		if err != nil {
			caData = nil
		}

		clusters = append(clusters, Cluster{
			Name:     c.Name,
			EnvName:  c.Name,
			Endpoint: c.Endpoint,
			CAData:   caData,
			Location: c.Location,
		})
	}
	return clusters, nil
}
