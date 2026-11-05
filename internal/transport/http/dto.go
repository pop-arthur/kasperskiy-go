package httptransport

import (
	"time"

	"kaspersky-go-test/internal/domain"
)

type createRequest struct {
	MD5         string
	Name        string
	Description *string
}

type updateRequest struct {
	Name        string
	Description *string
}

type threatResponse struct {
	MD5         string    `json:"md5"`
	Name        string    `json:"name"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type errorResponse struct {
	Message string `json:"message"`
	Details string `json:"details"`
}

func responseFrom(threat domain.Threat) threatResponse {
	return threatResponse{
		MD5: string(threat.MD5), Name: threat.Name, Description: threat.Description,
		CreatedAt: threat.CreatedAt.UTC(), UpdatedAt: threat.UpdatedAt.UTC(),
	}
}
