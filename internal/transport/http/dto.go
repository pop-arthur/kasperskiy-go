package httptransport

import "kaspersky-go-test/internal/domain"

func responseFrom(threat domain.Threat) ThreatResponse {
	return ThreatResponse{
		MD5: string(threat.MD5), Name: threat.Name, Description: threat.Description,
		CreatedAt: threat.CreatedAt.UTC(), UpdatedAt: threat.UpdatedAt.UTC(),
	}
}
