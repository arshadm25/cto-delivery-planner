package api

import "net/http"

// Placeholders filled in by Phase 4 (commitments, forecasts and workload).

// Delivery holds baseline, forecast and actual dates.
type Delivery struct{}

// Commitment is a planner-owned delivery commitment.
type Commitment struct{}

// WorkloadConcern is a person or period over capacity.
type WorkloadConcern struct{}

func (s *Server) addDelivery(r *http.Request, a *analysis) error { return nil }

func (s *Server) attachCommitments(r *http.Request, projectID int, ms []MilestoneView) error {
	return nil
}

func (s *Server) workloadConcerns(r *http.Request, a *analysis) ([]WorkloadConcern, error) {
	return []WorkloadConcern{}, nil
}
