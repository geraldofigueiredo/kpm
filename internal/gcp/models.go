package gcp

// Cluster represents a GKE cluster.
type Cluster struct {
	Name     string // full resource name
	EnvName  string // display name (same as Name; kept for UI grouping)
	Endpoint string // GKE master endpoint
	CAData   []byte // cluster CA certificate (base64-decoded)
	Location string // us-east4 etc
}
